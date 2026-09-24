package pgerr_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"uuid"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tyr-go/recipes/tasks/internal/dbtest"
	"github.com/tyr-go/recipes/tasks/pgerr"
	"github.com/tyr-go/recipes/tasks/store"
	"github.com/tyr-go/tyr"
)

// check fails t unless Map translates err into an error of the kind and
// the message of want, with err as its cause, or into nil if want is nil.
func check(t *testing.T, err error, want *tyr.Error) {
	t.Helper()
	mapped := pgerr.Map(err)
	if want == nil {
		if mapped != nil {
			t.Errorf("Map(%v) = %v, want nil", err, mapped)
		}
		return
	}
	e, ok := errors.AsType[*tyr.Error](mapped)
	if !ok || e.Kind != want.Kind || e.Message != want.Message || !errors.Is(e, err) {
		t.Errorf("Map(%v) = %v, want %v caused by it", err, mapped, want)
	}
}

func TestMap(t *testing.T) {
	// A failure to connect, without a database: a server that hangs up.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	_, connect := pgconn.Connect(t.Context(), "postgres://postgres@"+ln.Addr().String()+"/postgres")
	if _, ok := errors.AsType[*pgconn.ConnectError](connect); !ok {
		t.Fatalf("connecting to a server that hangs up: %v, want a *pgconn.ConnectError", connect)
	}

	tests := []struct {
		name string
		err  error
		want *tyr.Error
	}{
		{"no rows", fmt.Errorf("get: %w", pgx.ErrNoRows), tyr.NotFound("not found")},
		{"unique violation", &pgconn.PgError{Code: pgerrcode.UniqueViolation}, tyr.AlreadyExists("already exists")},
		{"foreign key violation", &pgconn.PgError{Code: pgerrcode.ForeignKeyViolation}, tyr.FailedPrecondition("conflicts with related records")},
		{"check violation", &pgconn.PgError{Code: pgerrcode.CheckViolation}, tyr.InvalidArgument("a value is invalid")},
		{"query canceled", &pgconn.PgError{Code: pgerrcode.QueryCanceled}, tyr.DeadlineExceeded("the query took too long")},
		{"serialization failure", &pgconn.PgError{Code: pgerrcode.SerializationFailure}, tyr.Unavailable("the query conflicted with another: try again")},
		{"deadlock", &pgconn.PgError{Code: pgerrcode.DeadlockDetected}, tyr.Unavailable("the query conflicted with another: try again")},
		{"wrapped", fmt.Errorf("create: %w", &pgconn.PgError{Code: pgerrcode.UniqueViolation}), tyr.AlreadyExists("already exists")},
		{"failure to connect", connect, tyr.Unavailable("the database is unavailable")},

		// Left to tyr: the context of the call knows what these mean.
		{"deadline", fmt.Errorf("timeout: %w", context.DeadlineExceeded), nil},
		{"canceled", context.Canceled, nil},
		// Left as internal errors.
		{"syntax error", &pgconn.PgError{Code: pgerrcode.SyntaxError}, nil},
		{"not of the database", errors.New("boom"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { check(t, tt.err, tt.want) })
	}
}

// TestMapFromDatabase checks that Map reads the errors of queries as pgx
// returns them from the server.
func TestMapFromDatabase(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	db := store.NewDB(pool)
	ctx := t.Context()
	project := store.CreateProjectParams{ID: uuid.NewV7(), OwnerID: "alice", Key: "WEB", Name: "Website"}
	if _, err := db.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		query func() error
		want  *tyr.Error
	}{
		{"no rows", func() error {
			_, err := db.GetProject(ctx, store.GetProjectParams{ID: uuid.NewV7(), OwnerID: "alice"})
			return err
		}, tyr.NotFound("not found")},
		{"the key taken", func() error {
			project.ID = uuid.NewV7()
			_, err := db.CreateProject(ctx, project)
			return err
		}, tyr.AlreadyExists("already exists")},
		{"a task of no project", func() error {
			_, err := db.CreateTask(ctx, store.CreateTaskParams{ID: uuid.NewV7(), ProjectID: uuid.NewV7(), Number: 1, Title: "Lost", Status: "todo"})
			return err
		}, tyr.FailedPrecondition("conflicts with related records")},
		{"a key in lowercase", func() error {
			_, err := db.CreateProject(ctx, store.CreateProjectParams{ID: uuid.NewV7(), OwnerID: "alice", Key: "web", Name: "Website"})
			return err
		}, tyr.InvalidArgument("a value is invalid")},
		{"the statement timeout", func() error {
			return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '10ms'"); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, "SELECT pg_sleep(1)")
				return err
			})
		}, tyr.DeadlineExceeded("the query took too long")},
		{"a serialization failure", func() error {
			_, err := pool.Exec(ctx, "DO $$ BEGIN RAISE EXCEPTION 'conflict' USING ERRCODE = 'serialization_failure'; END $$")
			return err
		}, tyr.Unavailable("the query conflicted with another: try again")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { check(t, tt.query(), tt.want) })
	}
}

func TestCode(t *testing.T) {
	err := fmt.Errorf("create: %w", &pgconn.PgError{Code: pgerrcode.UniqueViolation})
	if got := pgerr.Code(err); got != pgerrcode.UniqueViolation {
		t.Errorf("Code(%v) = %q, want %q", err, got, pgerrcode.UniqueViolation)
	}
	if got := pgerr.Code(pgx.ErrNoRows); got != "" {
		t.Errorf("Code(%v) = %q, want none", pgx.ErrNoRows, got)
	}
}
