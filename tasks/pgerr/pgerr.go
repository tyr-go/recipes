// Package pgerr translates the errors of PostgreSQL and pgx into errors of
// tyr, so that a query that fails answers the client with a kind, such as
// already_exists, rather than an internal error:
//
//	api.MapError(pgerr.Map)
//
// A service translates the errors it expects itself, with messages that
// name what it was about, such as a project that isn't found; Map
// translates the rest, with messages of its own.
package pgerr

import (
	"errors"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tyr-go/tyr"
)

// Map translates err, the error of a query, into a *tyr.Error of the kind
// it means for the client:
//
//   - [pgx.ErrNoRows]: not_found
//   - unique_violation (23505): already_exists
//   - foreign_key_violation (23503): failed_precondition
//   - check_violation (23514): invalid_argument
//   - query_canceled (57014), as by the statement_timeout of the
//     database: deadline_exceeded
//   - serialization_failure (40001) and deadlock_detected (40P01), which
//     a retry may pass: unavailable
//   - a failure to connect to the database: unavailable
//
// The *tyr.Error keeps err as its cause, for the log. Map returns nil for
// other errors, which tyr handles as errors that no mapper translates: an
// error of the context of the call gets its kind, deadline_exceeded or
// canceled, and the rest are internal.
func Map(err error) error {
	switch Code(err) {
	case pgerrcode.UniqueViolation:
		return tyr.AlreadyExists("already exists").WithCause(err)
	case pgerrcode.ForeignKeyViolation:
		return tyr.FailedPrecondition("conflicts with related records").WithCause(err)
	case pgerrcode.CheckViolation:
		return tyr.InvalidArgument("a value is invalid").WithCause(err)
	case pgerrcode.QueryCanceled:
		return tyr.DeadlineExceeded("the query took too long").WithCause(err)
	case pgerrcode.SerializationFailure, pgerrcode.DeadlockDetected:
		return tyr.Unavailable("the query conflicted with another: try again").WithCause(err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return tyr.NotFound("not found").WithCause(err)
	}
	if _, ok := errors.AsType[*pgconn.ConnectError](err); ok {
		return tyr.Unavailable("the database is unavailable").WithCause(err)
	}
	return nil
}

// Code returns the code of the error of PostgreSQL in err's chain, such as
// [pgerrcode.UniqueViolation], or "" if there is none.
func Code(err error) string {
	if e, ok := errors.AsType[*pgconn.PgError](err); ok {
		return e.Code
	}
	return ""
}
