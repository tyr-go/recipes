// Package auth authenticates the callers of the service by JSON Web
// Tokens and lets them call the operations whose roles they have.
//
// A token is signed with HS256 by a key that the service shares with
// whoever issues its tokens, such as the command token of this module in
// development. Its claims are the id of the caller, sub, its roles, and
// when it expires, exp:
//
//	{"sub": "alice", "roles": ["admin"], "exp": 1790276400, "iat": 1790272800}
//
// Authentication is HTTP middleware, Authenticate: it reads a header and
// only finds out who calls, never answering itself. Authorization is a tyr
// interceptor, Interceptor, which guards the operations that Require
// options mark, over every transport.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/tyr-go/tyr"
	"github.com/tyr-go/tyr/ctxkey"
)

// MinKeySize is the size of the smallest key that the package takes, in
// bytes: that of the output of SHA-256, as RFC 7518 requires of the keys
// of HS256.
const MinKeySize = 32

// Caller is an authenticated caller.
type Caller struct {
	ID    string   // the subject of the token
	Roles []string // such as "admin"
}

// HasRole reports whether c has the role.
func (c Caller) HasRole(role string) bool {
	return slices.Contains(c.Roles, role)
}

// The keys of the caller of a request and of what's wrong with its token.
var (
	callerKey  = ctxkey.New[Caller]("auth.caller")
	problemKey = ctxkey.New[string]("auth.problem")
)

// WithCaller returns a copy of ctx that carries c, as Authenticate makes
// for a request with a valid token.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return callerKey.Set(ctx, c)
}

// CallerFrom returns the caller that ctx carries.
func CallerFrom(ctx context.Context) (Caller, bool) {
	return callerKey.Get(ctx)
}

// claims are the claims of a token of the service.
type claims struct {
	Roles []string `json:"roles,omitempty"`
	jwt.RegisteredClaims
}

// leeway is how far the clocks of the service and of the issuer of its
// tokens may differ.
const leeway = 30 * time.Second

// NewToken returns a token for c that expires after ttl, signed with key.
// It panics if key is shorter than MinKeySize, c has no ID, or ttl isn't
// positive.
func NewToken(key []byte, c Caller, ttl time.Duration) string {
	checkKey("NewToken", key)
	switch {
	case c.ID == "":
		panic("auth: NewToken: the caller has no ID")
	case ttl <= 0:
		panic(fmt.Sprintf("auth: NewToken: ttl %v, want a positive duration", ttl))
	}
	now := time.Now()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Roles: c.Roles,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   c.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}).SignedString(key)
	if err != nil {
		panic("auth: NewToken: " + err.Error()) // a bug: an HMAC key signs anything
	}
	return token
}

// Authenticate returns HTTP middleware that puts the caller of a request
// with a valid bearer token, signed with key, into its context. A request
// without a valid token goes on without a caller: the operations that
// Require one reject it, telling why, such as an expired token, and the
// others serve it. Authenticate panics if key is shorter than MinKeySize.
func Authenticate(key []byte) func(http.Handler) http.Handler {
	checkKey("Authenticate", key)
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(), jwt.WithLeeway(leeway))
	keyFunc := func(*jwt.Token) (any, error) { return key, nil }
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				next.ServeHTTP(w, r)
				return
			}
			scheme, token, _ := strings.Cut(header, " ")
			if !strings.EqualFold(scheme, "Bearer") {
				next.ServeHTTP(w, r.WithContext(problemKey.Set(r.Context(), "the Authorization header needs a Bearer token")))
				return
			}
			var cl claims
			_, err := parser.ParseWithClaims(token, &cl, keyFunc)
			switch {
			case errors.Is(err, jwt.ErrTokenExpired):
				r = r.WithContext(problemKey.Set(r.Context(), "the bearer token has expired"))
			case err != nil || cl.Subject == "":
				r = r.WithContext(problemKey.Set(r.Context(), "the bearer token is invalid"))
			default:
				r = r.WithContext(WithCaller(r.Context(), Caller{ID: cl.Subject, Roles: cl.Roles}))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// checkKey panics if key is shorter than MinKeySize; fn names the function
// in the panic.
func checkKey(fn string, key []byte) {
	if len(key) < MinKeySize {
		panic(fmt.Sprintf("auth: %s: a key of %d bytes, want at least %d", fn, len(key), MinKeySize))
	}
}

// rolesKey marks the operations that need a caller, with the roles of
// which the caller needs one, if any.
var rolesKey = tyr.NewMetaKey[[]string]("auth.roles")

// Require returns an option for operations that only authenticated
// callers may call, and with roles, only callers who have one of them.
// Interceptor enforces it, and the option declares the errors that
// Interceptor returns, for the documents of the API:
//
//	ops := api.Group(auth.Require())       // any caller
//	admin := ops.Group(auth.Require("admin")) // a caller with the role admin
//
// The option of an operation replaces those of its groups.
func Require(roles ...string) tyr.OpOption {
	set := rolesKey.Option(slices.Clone(roles))
	errs := tyr.Errors(tyr.KindUnauthenticated)
	if len(roles) > 0 {
		errs = tyr.Errors(tyr.KindUnauthenticated, tyr.KindPermissionDenied)
	}
	return func(op *tyr.Operation) {
		set(op)
		errs(op)
	}
}

// Interceptor rejects the calls of the operations marked by Require that
// lack a caller, with unauthenticated, or whose caller lacks the roles,
// with permission_denied. It passes on the calls of other operations.
func Interceptor(ctx context.Context, op *tyr.Operation, req any, next tyr.Invoker) (any, error) {
	roles, ok := rolesKey.Get(op)
	if !ok {
		return next(ctx, req)
	}
	c, ok := CallerFrom(ctx)
	if !ok {
		problem, ok := problemKey.Get(ctx)
		if !ok {
			problem = "a bearer token is required"
		}
		return nil, tyr.Unauthenticated("%s", problem)
	}
	if len(roles) > 0 && !slices.ContainsFunc(roles, c.HasRole) {
		return nil, tyr.PermissionDenied("%s requires the role %s", op.Name(), strings.Join(roles, " or "))
	}
	return next(ctx, req)
}
