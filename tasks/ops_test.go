package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/tyr-go/recipes/tasks/contract"
	"github.com/tyr-go/recipes/tasks/internal/dbtest"
	"github.com/tyr-go/recipes/tasks/service"
	"github.com/tyr-go/recipes/tasks/store"
	"github.com/tyr-go/tyr/health"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestProbes(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	anyone := caller{t: t, client: startOn(t, store.NewDB(pool), nil)}
	probe := func(path string, status int, body string) {
		t.Helper()
		if resp, got := anyone.do("GET", path, ""); resp.StatusCode != status || got != body {
			t.Errorf("GET %s = %d %s, want %d %s", path, resp.StatusCode, got, status, body)
		}
	}
	probe("/health/live", 200, `{"status":"ok"}`)
	probe("/health/ready", 200, `{"status":"ok","checks":{"db":"ok"}}`)

	// Without its database, the service is alive but not ready.
	pool.Close()
	probe("/health/ready", 503, `{"status":"failed","checks":{"db":"failed"}}`)
	probe("/health/live", 200, `{"status":"ok"}`)
}

func TestCORS(t *testing.T) {
	t.Parallel()
	const app = "https://app.example.com"
	alice := as(t, startOn(t, store.NewDB(dbtest.New(t)), []string{app}), "alice")

	// A page of the app asks whether it may post JSON with a token, and
	// posts it; it may read the resource created and the request ID.
	resp, _ := alice.do("OPTIONS", "/projects", "", "Origin", app,
		"Access-Control-Request-Method", "POST", "Access-Control-Request-Headers", "authorization,content-type")
	if h := resp.Header; resp.StatusCode != 204 || h.Get("Access-Control-Allow-Origin") != app ||
		h.Get("Access-Control-Allow-Methods") != "POST" || h.Get("Access-Control-Allow-Headers") != "authorization,content-type" {
		t.Errorf("preflight = %d %v, want 204 with the headers of CORS", resp.StatusCode, h)
	}
	resp, body := alice.do("POST", "/projects", `{"key":"WEB","name":"Website"}`, "Origin", app)
	if h := resp.Header; resp.StatusCode != 201 || h.Get("Access-Control-Allow-Origin") != app || h.Get("Access-Control-Expose-Headers") != "Location, X-Request-ID" {
		t.Errorf("create from the app = %d %v %s, want 201 that the app may read", resp.StatusCode, h, body)
	}

	// A page of another site may do neither.
	resp, _ = alice.do("OPTIONS", "/projects", "", "Origin", "https://evil.example", "Access-Control-Request-Method", "POST")
	if resp.StatusCode != 403 || resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("preflight from another site = %d %v, want 403", resp.StatusCode, resp.Header)
	}
}

func TestDrain(t *testing.T) {
	t.Parallel()
	// Told to stop, the service fails readiness first and serves on for the
	// drain, closing connections after their responses; only then it
	// stops accepting connections. Neither the probes nor the document
	// need the database.
	synctest.Test(t, func(t *testing.T) {
		logger := slog.New(slog.DiscardHandler)
		ready := health.NewReadiness()
		srv := newServer("", newAPI(service.New(nil), logger), testKey, nil, ready, logger)
		ln := newPipeListener()
		ctx, stop := context.WithCancel(t.Context())
		served := make(chan error, 1)
		go func() { served <- serve(ctx, srv, ln, ready, 5*time.Second) }()

		client := &http.Client{Transport: &http.Transport{DialContext: ln.dial}}
		defer client.CloseIdleConnections()
		// get returns the status of a GET of path, 0 if it can't be sent,
		// and whether the service closes the connection after it.
		get := func(path string) (int, bool) {
			resp, err := client.Get(server + path)
			if err != nil {
				return 0, false
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return resp.StatusCode, resp.Close
		}
		if status, closed := get("/health/ready"); status != 200 || closed {
			t.Errorf("readiness = %d, closed %v; want 200 on a connection kept alive", status, closed)
		}

		stop()
		synctest.Wait()
		start := time.Now()
		for _, at := range []time.Duration{0, 4 * time.Second} {
			time.Sleep(at - time.Since(start))
			if status, _ := get("/health/ready"); status != 503 {
				t.Errorf("readiness %v into the drain = %d, want 503", at, status)
			}
			if status, _ := get("/health/live"); status != 200 {
				t.Errorf("liveness %v into the drain = %d, want 200", at, status)
			}
			if status, closed := get("/openapi.json"); status != 200 || !closed {
				t.Errorf("the document %v into the drain = %d, closed %v; want 200 on a connection closed after it", at, status, closed)
			}
		}

		if err := <-served; err != nil {
			t.Errorf("serve() = %v, want <nil>", err)
		}
		if took := time.Since(start); took != 5*time.Second {
			t.Errorf("serve() returned %v after the signal, want 5s", took)
		}
		if status, _ := get("/health/live"); status != 0 {
			t.Errorf("after the shutdown, liveness = %d, want no connection", status)
		}
	})
}

func TestTelemetry(t *testing.T) {
	// Not parallel: it sets the global provider of spans, which oteltyr and
	// otelpgx use, while the parallel tests wait.
	spans := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	pool, err := newPool(t.Context(), dbtest.Config(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	alice := as(t, startOn(t, store.NewDB(pool), nil), "alice")
	var web contract.Project
	alice.want(201, "POST", "/projects", `{"key":"WEB","name":"Website"}`, &web)
	alice.want(200, "POST", "/rpc", `{"jsonrpc":"2.0","method":"projects.get","params":{"id":"`+web.ID.String()+`"},"id":1}`, nil)

	ended := spans.Ended()
	find := func(name string) sdktrace.ReadOnlySpan {
		t.Helper()
		for _, s := range ended {
			if s.Name() == name {
				return s
			}
		}
		t.Fatalf("no span %q", name)
		return nil
	}
	// The span of a request of REST is named after its route, and the
	// queries of its operation are in its trace, named after their names
	// in sqlc.
	create := find("POST /projects")
	if create.SpanKind() != trace.SpanKindServer || !hasAttr(create, "tyr.operation", "projects.create") {
		t.Errorf("span of POST /projects: %v %v", create.SpanKind(), create.Attributes())
	}
	if insert := find("CreateProject"); insert.SpanContext().TraceID() != create.SpanContext().TraceID() {
		t.Error("the query CreateProject isn't in the trace of POST /projects")
	}

	// A call of JSON-RPC gets a span of its own under the request.
	rpc, get := find("POST /rpc"), find("projects.get")
	if get.SpanKind() != trace.SpanKindServer || get.Parent().SpanID() != rpc.SpanContext().SpanID() {
		t.Errorf("span of the call projects.get: %v under %v, want a server span under POST /rpc", get.SpanKind(), get.Parent().SpanID())
	}
	if query := find("GetProject"); query.SpanContext().TraceID() != rpc.SpanContext().TraceID() {
		t.Error("the query GetProject isn't in the trace of POST /rpc")
	}
}

// hasAttr reports whether s has the attribute key of the value.
func hasAttr(s sdktrace.ReadOnlySpan, key, value string) bool {
	for _, a := range s.Attributes() {
		if a.Key == attribute.Key(key) {
			return a.Value.AsString() == value
		}
	}
	return false
}

func TestQuerySpanName(t *testing.T) {
	tests := []struct{ sql, want string }{
		{"-- name: CreateProject :one\nINSERT INTO projects VALUES ($1)", "CreateProject"},
		{"begin", "BEGIN"},
		{"  select 1", "SELECT"},
		{"", "query"},
	}
	for _, tt := range tests {
		if got := querySpanName(tt.sql); got != tt.want {
			t.Errorf("querySpanName(%q) = %q, want %q", tt.sql, got, tt.want)
		}
	}
}

// pipeListener is a listener in memory, which a synctest bubble can serve
// on: dial makes a net.Pipe and hands one end to Accept.
type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	close  sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.close.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return pipeAddr{}
}

// dial connects to the listener, as the DialContext of an http.Transport.
func (l *pipeListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	server, client := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.closed:
		return nil, errors.New("connection refused")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// pipeAddr is the address of a pipeListener.
type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }
