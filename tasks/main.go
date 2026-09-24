// Command tasks serves the projects and tasks of its callers over REST and
// JSON-RPC, on PostgreSQL. The callers authenticate with tokens signed by
// the key of JWT_SECRET, of 32 bytes or more, which the command token of
// this module issues in development:
//
//	export DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable'
//	export JWT_SECRET=$(openssl rand -hex 32)
//	go run . -migrate
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
	"github.com/tyr-go/tyr/jsonrpc"
	"github.com/tyr-go/tyr/middleware"
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
	flag.Parse()

	// Records get the request ID and the operation of their context.
	logger := slog.New(tyr.NewLogHandler(slog.NewJSONHandler(os.Stderr, nil)))
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

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	if *migrate {
		if err := store.Migrate(ctx, pool, logger); err != nil {
			return fmt.Errorf("migrating the database: %w", err)
		}
	}

	api := newAPI(service.New(store.NewDB(pool)), logger)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           newHandler(api, key, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       time.Minute,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	logger.Info("serving", "addr", ln.Addr().String())

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	// The requests in flight get 10 seconds to finish.
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

// info describes the service in its documents.
var info = tyr.Info{
	Title:       "tasks",
	Version:     "1.0.0",
	Description: "Projects and their tasks, of the callers who create them.",
}

// newAPI returns the API of the service: its operations, the interceptor
// that authorizes their callers and the translation of the errors of the
// database.
func newAPI(svc *service.Service, logger *slog.Logger) *tyr.API {
	api := tyr.New(tyr.WithLogger(logger))
	api.Use(auth.Interceptor)
	api.MapError(pgerr.Map)

	// The contract has the names and the routes; who may call what is the
	// server's business. Every operation needs a caller.
	ops := api.Group(auth.Require())
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

// newHandler returns the HTTP handler of the service: the operations of api
// over REST and JSON-RPC, and their documents, behind the middleware that
// authenticates tokens signed with key.
func newHandler(api *tyr.API, key []byte, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	// The document tells of the same challenge that REST sends with 401.
	routes := rest.Mount(mux, api, rest.Challenge(`Bearer realm="tasks"`))
	mux.Handle("GET /openapi.json", routes.OpenAPI(info))
	mux.Handle("POST /rpc", jsonrpc.Handler(api, jsonrpc.Discover(info)))

	// The 404 and 405 of the mux are problems, as the errors of operations
	// are.
	return middleware.Chain(rest.ProblemHandler(mux), // first = outermost
		middleware.RequestID(),
		middleware.Logger(logger),
		middleware.Recover(logger),
		// It only finds out who calls: the interceptor of auth answers
		// the calls that need a caller and have none.
		auth.Authenticate(key),
	)
}
