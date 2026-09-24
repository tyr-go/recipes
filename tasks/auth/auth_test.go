package auth_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/tyr-go/recipes/tasks/auth"
	"github.com/tyr-go/tyr"
	"github.com/tyr-go/tyr/rest"
)

var key = []byte("an HS256 key of 32 bytes or more")

// newHandler returns a handler of three operations, whoami, which any
// caller may call, stats, which callers with the role admin may, and
// hello, which anyone may, behind Authenticate.
func newHandler() http.Handler {
	api := tyr.New()
	api.Use(auth.Interceptor)
	whoami := func(ctx context.Context, _ struct{}) (string, error) {
		c, _ := auth.CallerFrom(ctx)
		return c.ID + " " + strings.Join(c.Roles, ","), nil
	}
	api.Handle("whoami", whoami, rest.Route("GET /whoami"), auth.Require())
	api.Handle("stats", whoami, rest.Route("GET /stats"), auth.Require("admin"))
	api.Handle("hello", func(ctx context.Context, _ struct{}) (string, error) { return "hello", nil }, rest.Route("GET /hello"))
	mux := http.NewServeMux()
	rest.Mount(mux, api, rest.Challenge(`Bearer realm="tasks"`))
	return auth.Authenticate(key)(mux)
}

// get sends a GET of path with the Authorization header, if not empty, and
// returns the status and the body of the response, and its detail if it's
// a problem.
func get(h http.Handler, path, authorization string) (int, string) {
	req := httptest.NewRequest("GET", path, nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	return rec.Code, string(body)
}

// sign returns a token of the claims, signed with the method and the key.
func sign(t *testing.T, method jwt.SigningMethod, claims jwt.MapClaims, key any) string {
	t.Helper()
	token, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestAuthenticate(t *testing.T) {
	h := newHandler()
	alice := auth.NewToken(key, auth.Caller{ID: "alice"}, time.Hour)
	root := auth.NewToken(key, auth.Caller{ID: "root", Roles: []string{"admin"}}, time.Hour)
	exp := time.Now().Add(time.Hour).Unix()
	tests := []struct {
		name, path, authorization string
		status                    int
		body                      string // or its part
	}{
		{"a caller", "/whoami", "Bearer " + alice, 200, `"alice "`},
		{"a caller with roles", "/whoami", "Bearer " + root, 200, `"root admin"`},
		{"the scheme in lowercase", "/whoami", "bearer " + alice, 200, `"alice "`},
		{"no token", "/whoami", "", 401, `"detail":"a bearer token is required"`},
		{"another scheme", "/whoami", "Basic YWxpY2U6c2VjcmV0", 401, `"detail":"the Authorization header needs a Bearer token"`},
		{"not a token", "/whoami", "Bearer alice", 401, `"detail":"the bearer token is invalid"`},
		{"another key", "/whoami", "Bearer " + auth.NewToken([]byte("another key of 32 bytes, or more"), auth.Caller{ID: "alice"}, time.Hour), 401, `"detail":"the bearer token is invalid"`},
		{"another method", "/whoami", "Bearer " + sign(t, jwt.SigningMethodHS512, jwt.MapClaims{"sub": "alice", "exp": exp}, key), 401, `"detail":"the bearer token is invalid"`},
		{"no signature", "/whoami", "Bearer " + sign(t, jwt.SigningMethodNone, jwt.MapClaims{"sub": "alice", "exp": exp}, jwt.UnsafeAllowNoneSignatureType), 401, `"detail":"the bearer token is invalid"`},
		{"no subject", "/whoami", "Bearer " + sign(t, jwt.SigningMethodHS256, jwt.MapClaims{"exp": exp}, key), 401, `"detail":"the bearer token is invalid"`},
		{"no expiry", "/whoami", "Bearer " + sign(t, jwt.SigningMethodHS256, jwt.MapClaims{"sub": "alice"}, key), 401, `"detail":"the bearer token is invalid"`},
		{"a caller without the role", "/stats", "Bearer " + alice, 403, `"detail":"stats requires the role admin"`},
		{"a caller with the role", "/stats", "Bearer " + root, 200, `"root admin"`},
		{"an operation for anyone", "/hello", "", 200, `"hello"`},
		{"an operation for anyone, with a bad token", "/hello", "Bearer alice", 200, `"hello"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := get(h, tt.path, tt.authorization)
			if status != tt.status || !strings.Contains(body, tt.body) {
				t.Errorf("GET %s = %d %s, want %d with %s", tt.path, status, body, tt.status, tt.body)
			}
		})
	}
}

func TestExpiry(t *testing.T) {
	// The fake clock of synctest moves the token past its expiry at once.
	synctest.Test(t, func(t *testing.T) {
		h := newHandler()
		token := "Bearer " + auth.NewToken(key, auth.Caller{ID: "alice"}, time.Hour)
		// The clocks of the service and of the issuer may differ by 30
		// seconds.
		time.Sleep(time.Hour + 29*time.Second)
		if status, body := get(h, "/whoami", token); status != 200 {
			t.Errorf("within the leeway: %d %s, want 200", status, body)
		}
		time.Sleep(2 * time.Second)
		const expired = `"detail":"the bearer token has expired"`
		if status, body := get(h, "/whoami", token); status != 401 || !strings.Contains(body, expired) {
			t.Errorf("after the leeway: %d %s, want 401 with %s", status, body, expired)
		}
	})
}

func TestRequireDeclaresErrors(t *testing.T) {
	api := tyr.New()
	noop := func(ctx context.Context, _ struct{}) (struct{}, error) { return struct{}{}, nil }
	api.Handle("any", noop, auth.Require())
	api.Handle("admin", noop, auth.Require("admin"))
	want := map[string][]tyr.Kind{
		"any":   {tyr.KindUnauthenticated},
		"admin": {tyr.KindUnauthenticated, tyr.KindPermissionDenied},
	}
	for op := range api.Operations() {
		if got := op.Doc().Errors; !slices.Equal(got, want[op.Name()]) {
			t.Errorf("errors of %s = %v, want %v", op.Name(), got, want[op.Name()])
		}
	}
}

func TestWithCaller(t *testing.T) {
	c := auth.Caller{ID: "alice", Roles: []string{"admin"}}
	got, ok := auth.CallerFrom(auth.WithCaller(t.Context(), c))
	if !ok || got.ID != "alice" || !got.HasRole("admin") || got.HasRole("root") {
		t.Errorf("CallerFrom = %+v, %v; want %+v", got, ok, c)
	}
	if _, ok := auth.CallerFrom(t.Context()); ok {
		t.Error("CallerFrom of a context without a caller: found one")
	}
}

func TestPanics(t *testing.T) {
	short := []byte("31 bytes: one too few for HS256")
	tests := []struct {
		name string
		f    func()
		want string
	}{
		{"Authenticate with a short key", func() { auth.Authenticate(short) }, "auth: Authenticate: a key of 31 bytes, want at least 32"},
		{"NewToken with a short key", func() { auth.NewToken(short, auth.Caller{ID: "alice"}, time.Hour) }, "auth: NewToken: a key of 31 bytes, want at least 32"},
		{"NewToken without an ID", func() { auth.NewToken(key, auth.Caller{}, time.Hour) }, "auth: NewToken: the caller has no ID"},
		{"NewToken without a ttl", func() { auth.NewToken(key, auth.Caller{ID: "alice"}, 0) }, "auth: NewToken: ttl 0s, want a positive duration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if got := recover(); got != tt.want {
					t.Errorf("panicked with %v, want %q", got, tt.want)
				}
			}()
			tt.f()
		})
	}
}
