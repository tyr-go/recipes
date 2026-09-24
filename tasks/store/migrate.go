package store

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// migrations are the migrations of the schema, in the binary, for goose:
// a file per version, with the statements that apply it and those that
// undo it.
//
//go:embed migrations/*.sql
var migrations embed.FS

// Migrations returns the migrations of the schema, such as
// 00001_projects_and_tasks.sql, for tools that apply them themselves.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err) // a bug: the directory is embedded
	}
	return sub
}

// Migrate applies the migrations that the database lacks, in order, and
// logs each one to log. Instances of the service that start at once take
// turns, by an advisory lock of PostgreSQL, so that each migration runs
// once.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	// goose needs database/sql: a *sql.DB on the connections of the pool,
	// whose Close leaves the pool open.
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()
	p, err := goose.NewProvider(goose.DialectPostgres, db, Migrations(),
		goose.WithSessionLocker(locker), goose.WithSlog(log))
	if err != nil {
		return err
	}
	_, err = p.Up(ctx)
	return err
}
