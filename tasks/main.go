// Command tasks serves the projects and tasks of its callers over REST and
// JSON-RPC, on PostgreSQL. The callers authenticate with tokens signed by
// the key of JWT_SECRET, of 32 bytes or more, which the command token of
// this module issues in development:
//
//	export DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable'
//	export JWT_SECRET=$(openssl rand -hex 32)
//	go run . -migrate -origin http://localhost:5173
//
//	TOKEN=$(go run ./cmd/token -sub alice)
//	curl -i localhost:8080/projects -H "Authorization: Bearer $TOKEN" \
//		-H 'Content-Type: application/json' -d '{"key":"WEB","name":"Website"}'
//	curl -i localhost:8080/rpc -H "Authorization: Bearer $TOKEN" \
//		-H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","method":"projects.list","id":1}'
//
// It describes itself in an OpenAPI document and an OpenRPC one:
//
//	curl localhost:8080/openapi.json
//	curl localhost:8080/rpc -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","method":"rpc.discover","id":1}'
//
// Load balancers probe it past its middleware; readiness fails while the
// database doesn't answer, and when the service is told to stop, while it
// serves on for a while (see -drain):
//
//	curl localhost:8080/health/live
//	curl localhost:8080/health/ready
//
// With OTEL_EXPORTER_OTLP_ENDPOINT, such as http://localhost:4318, it sends
// traces and metrics to a collector of OpenTelemetry.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tyr-go/recipes/tasks/auth"
	"github.com/tyr-go/recipes/tasks/contract"
	"github.com/tyr-go/recipes/tasks/pgerr"
	"github.com/tyr-go/recipes/tasks/service"
	"github.com/tyr-go/recipes/tasks/store"
	"github.com/tyr-go/tyr"
	"github.com/tyr-go/tyr/health"
	"github.com/tyr-go/tyr/jsonrpc"
	"github.com/tyr-go/tyr/middleware"
	"github.com/tyr-go/tyr/oteltyr"
	"github.com/tyr-go/tyr/rest"
)

func main() {
	if err := run(); err != nil {
		slog.Error("tasks stopped", "err", err)
		os.Exit(1)
	}
}

// run runs the service until it gets SIGINT or SIGTERM.
func run() error {
	addr := flag.String("addr", ":8080", "the address to listen on")
	migrate := flag.Bool("migrate", false, "apply the migrations that the database lacks before serving")
	var origins []string
	flag.Func("origin", "an origin whose pages may call the service, such as https://app.example.com; repeat for more", func(origin string) error {
		origins = append(origins, origin)
		return nil
	})
	drain := flag.Duration("drain", 5*time.Second, "how long to serve on, with readiness failing, once told to stop")
	flag.Parse()

	// Records get the request ID and the operation of their context, and
	// the IDs of its trace.
	logger := slog.New(tyr.NewLogHandler(slog.NewJSONHandler(os.Stderr, nil), tyr.LogAttrs(oteltyr.TraceIDs)))
	slog.SetDefault(logger)

	// Secrets come from the environment: flags would show them in the
	// list of processes.
	url, key := os.Getenv("DATABASE_URL"), []byte(os.Getenv("JWT_SECRET"))
	switch {
	case url == "":
		return errors.New("DATABASE_URL is empty: set it to the URL of the database")
	case len(key) < auth.MinKeySize:
		return fmt.Errorf("JWT_SECRET has %d bytes: set it to a key of %d bytes or more", len(key), auth.MinKeySize)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// A second signal stops the service at once, rather than after the
	// drain.
	context.AfterFunc(ctx, stop)

	shutdownTelemetry, err := setupTelemetry(ctx)
	if err != nil {
		return fmt.Errorf("setting up OpenTelemetry: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(ctx); err != nil {
			logger.Error("stopping OpenTelemetry", "err", err)
		}
	}()

	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("DATABASE_URL: %w", err)
	}
	pool, err := newPool(ctx, config)
	if err != nil {
		return err
	}
	defer pool.Close()
	if *migrate {
		if err := store.Migrate(ctx, pool, logger); err != nil {
			return fmt.Errorf("migrating the database: %w", err)
		}
	}

	db := store.NewDB(pool)
	api := newAPI(service.New(db), logger)
	ready := health.NewReadiness(health.Check("db", db.Ping), health.WithLogger(logger))
	srv := newServer(*addr, api, key, origins, ready, logger)
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	logger.Info("serving", "addr", ln.Addr().String())
	return serve(ctx, srv, ln, ready, *drain)
}

// info describes the service in its documents.
var info = tyr.Info{
	Title:       "tasks",
	Version:     "1.0.0",
	Description: "Projects and their tasks, of the callers who create them.",
}

// mountOptions configure the routes of the service, and so its OpenAPI
// document: it tells of the same challenge that REST sends with 401.
var mountOptions = []rest.MountOption{rest.Challenge(`Bearer realm="tasks"`)}

//guide:new-api

// newAPI returns the API of the service: its operations, the interceptors
// that trace them and authorize their callers, and the translation of the
// errors of the database.
func newAPI(svc *service.Service, logger *slog.Logger) *tyr.API {
	api := tyr.New(tyr.WithLogger(logger))
	// First, so that its spans cover the others.
	api.Use(oteltyr.Interceptor(), auth.Interceptor)
	api.MapError(pgerr.Map)

	// The contract has the names and the routes; who may call what, and
	// for how long, is the server's business. Every operation needs a
	// caller and has 5 seconds, within the WriteTimeout of the server, and
	// may find the database unavailable.
	ops := api.Group(auth.Require(), tyr.Timeout(5*time.Second), tyr.Errors(tyr.KindUnavailable))
	ops.Implement(contract.CreateProject, svc.CreateProject)
	ops.Implement(contract.GetProject, svc.GetProject)
	ops.Implement(contract.ListProjects, svc.ListProjects)
	ops.Implement(contract.DeleteProject, svc.DeleteProject)
	ops.Implement(contract.CreateTask, svc.CreateTask)
	ops.Implement(contract.GetTask, svc.GetTask)
	ops.Implement(contract.ListTasks, svc.ListTasks)
	ops.Implement(contract.UpdateTask, svc.UpdateTask)
	ops.Implement(contract.DeleteTask, svc.DeleteTask)

	admin := ops.Group(auth.Require("admin"))
	admin.Implement(contract.GetStats, svc.GetStats)
	return api
}

//guide:end

//guide:new-server

// newServer returns the HTTP server of the service: the operations of api
// and their documents, the middleware around them, which authenticates
// tokens signed with key, and the probes past it, with the timeouts of a
// server that faces the internet. The pages of origins may call the
// service from browsers.
func newServer(addr string, api *tyr.API, key []byte, origins []string, ready *health.Readiness, logger *slog.Logger) *http.Server {
	mux := http.NewServeMux()
	routes := rest.Mount(mux, api, mountOptions...)
	mux.Handle("GET /openapi.json", routes.OpenAPI(info))
	mux.Handle("POST /rpc", jsonrpc.Handler(api, jsonrpc.Discover(info)))

	cors := middleware.CORS{
		Origins: origins,
		// The resource that a create makes, and the ID of a request to
		// report.
		Expose: []string{"Location", "X-Request-ID"},
		MaxAge: time.Hour,
	}
	// No protection against cross-site requests: the service reads its
	// callers from the header Authorization, which a browser doesn't add
	// to a request by itself, as it does cookies, so a page of another
	// site can't call it on its user's behalf.

	// The probes go past the middleware: the balancers call them every few
	// seconds, and the access log would drown in their records.
	root := http.NewServeMux()
	root.Handle("GET /health/live", health.Live())
	root.Handle("GET /health/ready", ready)
	root.Handle("/", oteltyr.Handler(middleware.Chain(rest.ProblemHandler(mux), // first = outermost
		middleware.RequestID(),
		middleware.Logger(logger),
		// It answers preflight requests before the mux, which would
		// answer them with 405, and Recover keeps its headers on a 500.
		cors.Handler,
		middleware.Recover(logger),
		// It only finds out who calls: the interceptor of auth answers
		// the calls that need a caller and have none.
		auth.Authenticate(key),
	)))

	return &http.Server{
		Addr:              addr,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       time.Minute,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

//guide:end

// serve serves srv on ln until ctx is done, then shuts srv down
// gracefully. First it drains: ready fails, and srv serves on for drain,
// while the balancers take the traffic away, closing connections after
// their responses, so that clients reconnect elsewhere. Then srv stops
// accepting connections, and serve waits up to 10 seconds for the requests
// in flight.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, ready *health.Readiness, drain time.Duration) error {
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), drain+10*time.Second)
	defer cancel()
	srv.SetKeepAlivesEnabled(false)
	ready.Drain(ctx, drain)
	if err := srv.Shutdown(ctx); err != nil {
		return err
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
