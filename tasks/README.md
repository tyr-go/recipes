# tasks

A service of projects and their tasks on PostgreSQL, built with [tyr](https://github.com/tyr-go/tyr): one contract served over REST and JSON-RPC, and called by a typed client. Its callers authenticate with JSON Web Tokens and see their own projects only.

## Run it

It needs PostgreSQL 17 or later, and a key of 32 bytes or more that signs its tokens:

```sh
docker run --rm -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:17
export DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable'
export JWT_SECRET=$(openssl rand -hex 32)
go run . -migrate -origin http://localhost:5173
```

`-migrate` applies the migrations that the database lacks before serving, and `-origin`, which repeats, names an origin whose pages may call the service from browsers. In another terminal, with the same `JWT_SECRET`, the command `token` issues a token, as the service's identity provider would:

```sh
TOKEN=$(go run ./cmd/token -sub alice)
curl -i localhost:8080/projects -H "Authorization: Bearer $TOKEN" \
	-H 'Content-Type: application/json' -d '{"key":"WEB","name":"Website"}'
curl -i localhost:8080/rpc -H "Authorization: Bearer $TOKEN" \
	-H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","method":"projects.list","id":1}'
```

It describes itself at `/openapi.json`, and to `rpc.discover` over JSON-RPC.

## Operations

| Operation | REST | Errors of its own |
|---|---|---|
| `projects.create` | `POST /projects`: 201 with a `Location` | `already_exists`: the caller has a project of the key |
| `projects.get` | `GET /projects/{id}` | `not_found` |
| `projects.list` | `GET /projects?limit=&cursor=` | |
| `projects.delete` | `DELETE /projects/{id}`: 204 | `not_found`; `failed_precondition`: the project has tasks |
| `tasks.create` | `POST /projects/{project_id}/tasks`: 201 with a `Location` | `not_found` |
| `tasks.get` | `GET /tasks/{id}` | `not_found` |
| `tasks.list` | `GET /projects/{project_id}/tasks?status=&limit=&cursor=` | `not_found` |
| `tasks.update` | `PATCH /tasks/{id}` | `not_found` |
| `tasks.delete` | `DELETE /tasks/{id}`: 204 | `not_found` |
| `admin.stats` | `GET /admin/stats` | |

JSON-RPC serves the same operations at `POST /rpc`, by their names. Every operation needs a token, or it fails with `unauthenticated`, and `admin.stats` a caller with the role `admin`, or it fails with `permission_denied`. A request that fails its checks gets `invalid_argument`, with a violation per field.

## How it's built

| Where | What |
|---|---|
| [contract](contract) | The operations, with their requests, results and documentation: the contract of the server and its clients |
| [service](service) | The handlers: a plain function per operation, of a request, that returns a result |
| [auth](auth) | Tokens: the middleware that finds out who calls, the interceptor that lets them call what they may, and the tokens of `cmd/token` |
| [store](store) | The schema, in migrations, and the queries, in SQL, from which [sqlc](https://sqlc.dev) generates the Go code on pgx |
| [pgerr](pgerr) | The errors of PostgreSQL as kinds of tyr |
| [main.go](main.go) | The API: the operations, their groups, the interceptors and the translation of errors; the middleware, the probes and the server, which drains before it stops |
| [telemetry.go](telemetry.go) | OpenTelemetry: the providers, and the tracer of the queries |
| [api](api) | The OpenAPI and OpenRPC documents of the service, which clients are generated from |
| [clients/ts](clients/ts) | A typed client in TypeScript, generated from the OpenAPI document |
| [cmd/token](cmd/token) | A token for development |
| [internal/dbtest](internal/dbtest) | A database of its own for each test |

### Authentication and authorization

A token is signed with HS256 by `JWT_SECRET`, and its claims are the id of the caller, `sub`, its roles and when it expires, `exp`. The middleware `auth.Authenticate` only finds out who calls: a request with a valid token goes on with its caller in the context, and one without goes on without. It never answers itself. The interceptor `auth.Interceptor` decides, for the operations that the option `auth.Require` marks, over REST and JSON-RPC alike, and says what's wrong, such as an expired token:

```go
ops := api.Group(auth.Require())          // every operation needs a caller
admin := ops.Group(auth.Require("admin")) // admin.stats one with the role admin
```

A caller owns the projects it creates and their tasks. Every query has the owner in its `WHERE`, so the projects of others are as good as missing: not found, rather than forbidden, which would tell they exist.

### Pages

The lists are newest first, a page at a time: 20 items, or `limit`, up to 100. A page that isn't the last has a `next_cursor`, which the request of the next page sends as its `cursor`. The cursor is the id of the last item of the page, and the next page is the items after it, by `WHERE id < $cursor ORDER BY id DESC`, which an index serves however deep the page. Ids are UUIDv7s of the package `uuid` of Go 1.27, which grow with time, and a JavaScript client reads them as strings, without the loss of precision of 64-bit numbers.

### Transactions

`store.DB.InTx` runs queries in a transaction. A task gets the next number of its project from an update of the project, which locks its row until the transaction ends: tasks created at once get numbers one after another, and a task that fails to be created gives its number back.

```go
err = s.db.InTx(ctx, func(q *store.Queries) error {
	n, err := q.NextTaskNumber(ctx, store.NextTaskNumberParams{ID: req.ProjectID, OwnerID: owner})
	if errors.Is(err, pgx.ErrNoRows) {
		return projectNotFound(req.ProjectID)
	}
	...
	task, err = q.CreateTask(ctx, store.CreateTaskParams{ID: uuid.NewV7(), ProjectID: req.ProjectID, Number: n, ...})
	return err
})
```

### Errors of the database

A handler translates the errors it expects itself, with a message that says what it's about, such as a project that isn't found, and returns the others as they are. `pgerr.Map`, which `api.MapError` gets, translates those of the database:

| Error | Kind |
|---|---|
| `pgx.ErrNoRows` | `not_found` |
| unique_violation (23505) | `already_exists` |
| foreign_key_violation (23503) | `failed_precondition` |
| check_violation (23514) | `invalid_argument` |
| query_canceled (57014), as by `statement_timeout` | `deadline_exceeded` |
| serialization_failure (40001), deadlock_detected (40P01) | `unavailable`: a retry may pass |
| a failure to connect | `unavailable` |

It leaves other errors to tyr: an error of the context of the call keeps its kind, `deadline_exceeded` or `canceled`, and the rest are internal errors, which the client sees without their details.

### The schema and the queries

The migrations of [goose](https://github.com/pressly/goose) are in the binary, and `store.Migrate` takes an advisory lock of PostgreSQL, so that instances that start at once apply each migration once. After a change to the migrations or the queries, run `sqlc generate` (sqlc 1.31) and commit the code; CI checks that it is up to date with `sqlc diff`.

### Clients

A client in Go calls the operations of the contract with the typed client of tyr, over JSON-RPC, as the tests do; a token goes in a header, by a transport of its own, such as `bearer` of [main_test.go](main_test.go):

```go
tasks := jsonrpc.NewClient("http://tasks.internal/rpc", &http.Client{Transport: bearer{token, http.DefaultTransport}})
page, err := tasks.Call(ctx, contract.ListTasks, contract.ListTasksReq{ProjectID: id, Status: "todo"})
```

A client in TypeScript is generated from the OpenAPI document, in [api](api): [openapi-typescript](https://openapi-ts.dev) makes its types, and [openapi-fetch](https://openapi-ts.dev/openapi-fetch/) calls the service with them. The errors are typed by status, so the kind of an error tells a client which one it is:

```ts
const { data, error } = await client.POST("/projects", { body: { key, name } });
if (error?.kind === "already_exists") {
	// a 409 of projects.create: the caller has a project of the key
}
```

[clients/ts](clients/ts) is such a client, which CI compiles, with checks of the types of the errors. The documents in `api` are those of the API: a test fails when they aren't, and CI checks that the types of the client are those of the document. After a change to the contract:

```sh
go test -run TestDocumentFiles -update .
cd clients/ts && npm run generate
```

### In production

- **Probes.** `/health/live` answers 200 while the process serves, and `/health/ready` 200 while the database answers a ping within a second, and 503 otherwise. They go past the middleware, on an outer mux, as balancers call them every few seconds and the access log would drown in their records.
- **Drain.** Told to stop by SIGTERM, the service fails readiness and serves on for `-drain`, 5 seconds by default, closing connections after their responses, while the balancers take the traffic away; then it stops accepting connections and gives the requests in flight 10 seconds. A second signal stops it at once.
- **Timeouts.** An operation has 5 seconds, by `tyr.Timeout` on the group of all of them, over REST and JSON-RPC alike; its queries end with its context, and the client gets `deadline_exceeded`. The database can stop a query of its own accord too: add `&statement_timeout=10s` to `DATABASE_URL`, and `pgerr.Map` gives such a query `deadline_exceeded` as well. The server has the timeouts of one that faces the internet: 5 seconds for the headers of a request, 10 for all of it and for the response.
- **CORS.** The pages of the origins of `-origin` may call the service: CORS answers their preflight requests and lets them read the `Location` of a create and the request ID. There's no protection against cross-site requests, as the service reads its callers from the header `Authorization`, which a browser doesn't add to a request by itself, as it does cookies.
- **OpenTelemetry.** With `OTEL_EXPORTER_OTLP_ENDPOINT`, such as `http://localhost:4318`, the service sends spans and metrics to a collector over OTLP, and the other variables of the SDK configure the rest, such as `OTEL_SERVICE_NAME`. The span of a request is named after its route, such as `GET /projects/{id}`, a call of JSON-RPC gets a span of its own, and each query one named after its name in sqlc, such as `GetProject`, in the trace of the request. The log records have the IDs of the trace, `trace_id` and `span_id`.

## Test it

The tests run on a real PostgreSQL, which `DATABASE_URL` names. Each test gets a database of its own, a copy of a template that has the migrations, so they run in parallel:

```sh
DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' go test ./...
```

Without `DATABASE_URL`, the tests of the database skip. CI sets `REQUIRE_DB=1`, which makes them fail instead, so that a missing database can't pass for a green run. The tests of the service go through its whole HTTP handler, over REST and over JSON-RPC with the typed client of the contract, and check the probes, CORS, the drain, with the fake clock of `testing/synctest`, and the spans of a request.
