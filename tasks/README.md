# tasks

A service of projects and tasks on PostgreSQL, built with [tyr](https://github.com/tyr-go/tyr). This first part is its store: the schema, the queries and the errors of the database. The operations come next.

## Test it

The tests of the database run on a real PostgreSQL, 17 or later, which `DATABASE_URL` names. Each test gets a database of its own, a copy of a template that has the migrations, so they run in parallel:

```sh
docker run --rm -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:17
DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' go test ./...
```

Without `DATABASE_URL`, those tests skip. CI sets `REQUIRE_DB=1`, which makes them fail instead, so that a missing database can't pass for a green run.

## The store

| Where | What |
|---|---|
| [store/migrations](store/migrations) | The schema, in migrations of [goose](https://github.com/pressly/goose): a file per version, with the statements that apply it and those that undo it |
| [store/queries](store/queries) | The queries, in SQL |
| [store](store) | The Go code that [sqlc](https://sqlc.dev) generates from both: a method per query, with the types of its rows and parameters; `DB` runs them one by one or in a transaction, `InTx`, and `Migrate` applies the migrations the database lacks |
| [pgerr](pgerr) | `pgerr.Map`, which translates the errors of PostgreSQL into kinds of tyr, for `api.MapError` |
| [internal/dbtest](internal/dbtest) | A database of its own for each test |

After a change to the migrations or the queries, run `sqlc generate` (sqlc 1.31) and commit the code. CI checks that it is up to date with `sqlc diff`.

The migrations are in the binary, and `Migrate` takes an advisory lock of PostgreSQL, so that instances that start at once apply each migration once.

Ids are UUIDv7s of the package `uuid` of Go 1.27: they grow with time, so the newest rows come first by id, and a JavaScript client reads them as strings, without the loss of precision of 64-bit numbers.

### Errors of the database

A service translates the errors it expects itself, with messages that name what they were about, such as a project that isn't found. `pgerr.Map` translates the rest:

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
