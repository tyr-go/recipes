// Package store keeps the projects and tasks of the service in PostgreSQL.
//
// The queries are SQL, in queries/, and sqlc generates the Go code that
// runs them, from the queries and the schema: [Queries], with a method per
// query, and the types of their rows and parameters. The schema is the sum
// of the migrations, in migrations/, which [Migrate] applies. After a
// change to either, run sqlc generate in the directory of the module.
//
// A query that finds no row returns [github.com/jackc/pgx/v5.ErrNoRows],
// and one that PostgreSQL rejects a [*github.com/jackc/pgx/v5/pgconn.PgError]
// with its code, such as 23505 for a unique violation: package pgerr reads
// them.
package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB runs the queries on a pool of connections, one by one or in a
// transaction.
type DB struct {
	*Queries
	pool *pgxpool.Pool
}

// NewDB returns a DB on pool.
func NewDB(pool *pgxpool.Pool) *DB {
	return &DB{Queries: New(pool), pool: pool}
}

// InTx runs fn in a transaction: the queries fn runs on q are committed
// together if fn returns nil, and rolled back if it returns an error,
// which InTx returns as it is, or panics. The transaction has the default
// isolation of PostgreSQL, read committed.
func (db *DB) InTx(ctx context.Context, fn func(q *Queries) error) error {
	return pgx.BeginFunc(ctx, db.pool, func(tx pgx.Tx) error {
		return fn(db.WithTx(tx))
	})
}

// Ping checks that the database answers, for the readiness of the service.
func (db *DB) Ping(ctx context.Context) error {
	return db.pool.Ping(ctx)
}
