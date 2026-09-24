// Package dbtest gives each test a database of its own, with the schema of
// the migrations, on the PostgreSQL server of DATABASE_URL, so that tests
// run in parallel on a real database without seeing each other's rows.
//
// A database is a copy of a template that has the migrations applied, and
// CREATE DATABASE makes a copy in milliseconds. The template is made once
// per version of the migrations and kept on the server for later runs: its
// name has a hash of them.
package dbtest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tyr-go/recipes/tasks/store"
)

// New returns a pool of connections to a new database for t, with the
// schema of the migrations; the database is dropped when t ends. Without
// DATABASE_URL, New skips t, unless REQUIRE_DB is set, as in CI, where a
// test that skipped the database would pass without testing it.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		if os.Getenv("REQUIRE_DB") != "" {
			t.Fatal("dbtest: REQUIRE_DB is set, but DATABASE_URL isn't")
		}
		t.Skip("dbtest: set DATABASE_URL to a PostgreSQL server, such as postgres://postgres@localhost:5432/postgres, to run the tests of the database")
	}
	s, err := connect()
	if err != nil {
		t.Fatal(err)
	}

	name := "tasks_test_" + strings.ToLower(rand.Text())
	ctx := context.Background()
	if _, err := s.admin.Exec(ctx, "CREATE DATABASE "+ident(name)+" TEMPLATE "+ident(s.template)); err != nil {
		t.Fatalf("dbtest: creating the database of the test: %v", err)
	}
	config := s.config.Copy()
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		// FORCE ends the sessions the test left, if any.
		if _, err := s.admin.Exec(ctx, "DROP DATABASE "+ident(name)+" WITH (FORCE)"); err != nil {
			t.Errorf("dbtest: dropping the database of the test: %v", err)
		}
	})
	return pool
}

// server is the PostgreSQL server of the tests: the configuration of
// DATABASE_URL, a pool on its database, and the template on the server.
type server struct {
	config   *pgxpool.Config
	admin    *pgxpool.Pool
	template string
}

// connect returns the server of DATABASE_URL, with a template of the
// current migrations. The tests of a package share it.
var connect = sync.OnceValues(func() (*server, error) {
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, fmt.Errorf("dbtest: DATABASE_URL: %w", err)
	}
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	s := &server{config: config, admin: admin}
	if s.template, err = s.makeTemplate(ctx); err != nil {
		admin.Close()
		return nil, fmt.Errorf("dbtest: making the template: %w", err)
	}
	return s, nil
})

// lockID is the key of the advisory lock that the test binaries of the
// packages, which go test runs at once, take to make the template one at a
// time.
const lockID = 0x7461736b73 // "tasks"

// makeTemplate returns the name of the template of the current migrations
// on the server, which it makes if the server lacks it, and drops the
// templates of earlier migrations.
func (s *server) makeTemplate(ctx context.Context) (string, error) {
	hash, err := hashMigrations()
	if err != nil {
		return "", err
	}
	template := "tasks_template_" + hash

	conn, err := s.admin.Acquire(ctx) // the lock is of a session
	if err != nil {
		return "", err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
		return "", err
	}
	defer func() {
		// Were it to fail, the lock would go with the session, at exit.
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockID)
	}()

	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT FROM pg_database WHERE datname = $1)", template).Scan(&exists); err != nil {
		return "", err
	}
	if !exists {
		// Migrate a database of another name and rename it once it's
		// done: a run that fails halfway leaves no template without all
		// the migrations.
		building := template + "_new"
		for _, sql := range []string{
			"DROP DATABASE IF EXISTS " + ident(building) + " WITH (FORCE)",
			"CREATE DATABASE " + ident(building),
		} {
			if _, err := conn.Exec(ctx, sql); err != nil {
				return "", err
			}
		}
		if err := s.migrate(ctx, building); err != nil {
			return "", err
		}
		if _, err := conn.Exec(ctx, "ALTER DATABASE "+ident(building)+" RENAME TO "+ident(template)); err != nil {
			return "", err
		}
	}

	// The templates of earlier migrations are of no more use.
	rows, err := conn.Query(ctx, "SELECT datname FROM pg_database WHERE datname LIKE 'tasks\\_template\\_%' AND datname <> $1", template)
	if err != nil {
		return "", err
	}
	old, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", err
	}
	for _, name := range old {
		if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+ident(name)+" WITH (FORCE)"); err != nil {
			return "", err
		}
	}
	return template, nil
}

// migrate applies the migrations to the database name.
func (s *server) migrate(ctx context.Context, name string) error {
	config := s.config.Copy()
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return err
	}
	defer pool.Close() // before the rename, which needs the database idle
	return store.Migrate(ctx, pool, slog.New(slog.DiscardHandler))
}

// hashMigrations returns a hash of the names and the contents of the
// migrations, short enough for the name of a database.
func hashMigrations() (string, error) {
	h := sha256.New()
	err := fs.WalkDir(store.Migrations(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(store.Migrations(), path)
		if err != nil {
			return err
		}
		h.Write(fmt.Appendf(nil, "%s %d\n", path, len(data)))
		h.Write(data)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:16], err
}

// ident quotes the name of a database for SQL.
func ident(name string) string {
	return pgx.Identifier{name}.Sanitize()
}
