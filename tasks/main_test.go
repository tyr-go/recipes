package main

import (
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tyr-go/recipes/tasks/auth"
	"github.com/tyr-go/recipes/tasks/contract"
	"github.com/tyr-go/recipes/tasks/internal/dbtest"
	"github.com/tyr-go/recipes/tasks/service"
	"github.com/tyr-go/recipes/tasks/store"
	"github.com/tyr-go/tyr"
	"github.com/tyr-go/tyr/jsonrpc"
)

// testKey signs the tokens of the tests.
var testKey = []byte("the key of the tests of the tasks")

// server is where the client of newServer finds the service: over the
// network in memory of httptest, any host does.
const server = "http://example.com"

// newServer returns a client of a server of the service, on a database of
// its own.
func newServer(t *testing.T) *http.Client {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	api := newAPI(service.New(store.NewDB(dbtest.New(t))), logger)
	return httptest.NewTestServer(t, newHandler(api, testKey, logger)).Client()
}

// caller calls the service over REST with a token, or without one if
// token is empty.
type caller struct {
	t      *testing.T
	client *http.Client
	token  string
}

// as returns a caller with a token of the id and the roles.
func as(t *testing.T, client *http.Client, id string, roles ...string) caller {
	return caller{t: t, client: client, token: auth.NewToken(testKey, auth.Caller{ID: id, Roles: roles}, time.Hour)}
}

// do sends a request with body, as JSON if it isn't empty, and returns the
// response and its body.
func (c caller) do(method, path, body string) (*http.Response, string) {
	c.t.Helper()
	req, err := http.NewRequestWithContext(c.t.Context(), method, server+path, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return resp, string(data)
}

// want fails the test unless the request gets the status, and decodes the
// body into dst if it isn't nil.
func (c caller) want(status int, method, path, body string, dst any) *http.Response {
	c.t.Helper()
	resp, data := c.do(method, path, body)
	if resp.StatusCode != status {
		c.t.Fatalf("%s %s = %d %s, want %d", method, path, resp.StatusCode, data, status)
	}
	if dst != nil {
		if err := json.Unmarshal([]byte(data), dst); err != nil {
			c.t.Fatalf("%s %s: %v in %s", method, path, err, data)
		}
	}
	return resp
}

// problem fails the test unless the request gets the problem, compact
// JSON as the server writes it.
func (c caller) problem(method, path, body, problem string) {
	c.t.Helper()
	resp, data := c.do(method, path, body)
	if resp.Header.Get("Content-Type") != "application/problem+json" || data != problem {
		c.t.Errorf("%s %s = %d %s, want %s", method, path, resp.StatusCode, data, problem)
	}
}

func TestProjects(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	alice, bob := as(t, srv, "alice"), as(t, srv, "bob")

	var web contract.Project
	resp := alice.want(201, "POST", "/projects", `{"key":"WEB","name":"Website"}`, &web)
	if loc := resp.Header.Get("Location"); web.Key != "WEB" || web.Name != "Website" || web.CreatedAt.IsZero() || loc != "/projects/"+web.ID.String() {
		t.Errorf("created %+v at %s", web, loc)
	}
	var got contract.Project
	if alice.want(200, "GET", "/projects/"+web.ID.String(), "", &got); got != web {
		t.Errorf("GET = %+v, want %+v", got, web)
	}

	// Keys are unique among the projects of an owner.
	alice.problem("POST", "/projects", `{"key":"WEB","name":"Again"}`,
		`{"type":"/problems/already_exists","title":"Already Exists","status":409,"detail":"a project with the key WEB exists","kind":"already_exists"}`)
	bob.want(201, "POST", "/projects", `{"key":"WEB","name":"Bob's website"}`, nil)
	alice.problem("POST", "/projects", `{"key":"web","name":"Website"}`,
		`{"type":"/problems/invalid_argument","title":"Invalid Argument","status":400,"detail":"validation failed","kind":"invalid_argument",`+
			`"errors":[{"pointer":"/key","detail":"must be of A-Z and 0-9, starting with a letter"}]}`)

	// The projects of others are as good as missing.
	notFound := `{"type":"/problems/not_found","title":"Not Found","status":404,"detail":"project ` + web.ID.String() + ` not found","kind":"not_found"}`
	bob.problem("GET", "/projects/"+web.ID.String(), "", notFound)
	bob.problem("DELETE", "/projects/"+web.ID.String(), "", notFound)

	alice.want(204, "DELETE", "/projects/"+web.ID.String(), "", nil)
	alice.problem("GET", "/projects/"+web.ID.String(), "", notFound)
	if resp, body := alice.do("GET", "/projects/web", ""); resp.StatusCode != 400 {
		t.Errorf("GET of a project by a key = %d %s, want 400: the id is a UUID", resp.StatusCode, body)
	}
}

func TestListProjects(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	alice := as(t, srv, "alice")
	var keys []string // newest first
	for _, key := range []string{"ONE", "TWO", "THREE", "FOUR", "FIVE"} {
		alice.want(201, "POST", "/projects", `{"key":"`+key+`","name":"Project"}`, nil)
		keys = append([]string{key}, keys...)
	}
	as(t, srv, "bob").want(201, "POST", "/projects", `{"key":"BOB","name":"Project"}`, nil)

	// Pages of two, each after the one before, until one has no cursor.
	var got []string
	path := "/projects?limit=2"
	for pages := 0; path != ""; pages++ {
		if pages == 5 {
			t.Fatalf("more than 5 pages of 5 projects: %v", got)
		}
		var page contract.Page[contract.Project]
		alice.want(200, "GET", path, "", &page)
		for _, p := range page.Items {
			got = append(got, p.Key)
		}
		path = ""
		if page.NextCursor != "" {
			path = "/projects?limit=2&cursor=" + page.NextCursor
		}
	}
	if strings.Join(got, " ") != strings.Join(keys, " ") {
		t.Errorf("pages of alice's projects: %v, want %v", got, keys)
	}

	var all contract.Page[contract.Project]
	if alice.want(200, "GET", "/projects", "", &all); len(all.Items) != 5 || all.NextCursor != "" {
		t.Errorf("a page of 20 by default: %d projects, cursor %q; want 5 and none", len(all.Items), all.NextCursor)
	}
	alice.problem("GET", "/projects?cursor=nope", "",
		`{"type":"/problems/invalid_argument","title":"Invalid Argument","status":400,"detail":"validation failed","kind":"invalid_argument",`+
			`"errors":[{"pointer":"/cursor","detail":"must be the next_cursor of a page"}]}`)
	alice.problem("GET", "/projects?limit=101", "",
		`{"type":"/problems/invalid_argument","title":"Invalid Argument","status":400,"detail":"validation failed","kind":"invalid_argument",`+
			`"errors":[{"pointer":"/limit","detail":"must be at most 100"}]}`)
}

func TestTasks(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	alice, bob := as(t, srv, "alice"), as(t, srv, "bob")
	var web contract.Project
	alice.want(201, "POST", "/projects", `{"key":"WEB","name":"Website"}`, &web)
	tasks := "/projects/" + web.ID.String() + "/tasks"

	// A task gets the next number of its project.
	var logo, copyTask contract.Task
	resp := alice.want(201, "POST", tasks, `{"title":"Draw a logo","due_at":"2026-10-01T18:00:00Z"}`, &logo)
	if loc := resp.Header.Get("Location"); logo.Number != 1 || logo.Status != "todo" || logo.ProjectID != web.ID ||
		logo.DueAt == nil || !logo.DueAt.Equal(time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)) || loc != "/tasks/"+logo.ID.String() {
		t.Errorf("created %+v at %s", logo, loc)
	}
	alice.want(201, "POST", tasks, `{"title":"Write the copy","status":"doing"}`, &copyTask)
	if copyTask.Number != 2 || copyTask.Status != "doing" || copyTask.DueAt != nil {
		t.Errorf("created %+v", copyTask)
	}
	bob.problem("POST", tasks, `{"title":"Mine now"}`,
		`{"type":"/problems/not_found","title":"Not Found","status":404,"detail":"project `+web.ID.String()+` not found","kind":"not_found"}`)

	// A change sets the fields it has.
	var done contract.Task
	alice.want(200, "PATCH", "/tasks/"+logo.ID.String(), `{"status":"done"}`, &done)
	if done.Status != "done" || done.Title != logo.Title || !done.DueAt.Equal(*logo.DueAt) || !done.UpdatedAt.After(logo.UpdatedAt) {
		t.Errorf("changed %+v into %+v", logo, done)
	}
	alice.problem("PATCH", "/tasks/"+logo.ID.String(), `{}`,
		`{"type":"/problems/invalid_argument","title":"Invalid Argument","status":400,"detail":"nothing to change: set title, status or due_at","kind":"invalid_argument"}`)
	bob.problem("PATCH", "/tasks/"+logo.ID.String(), `{"status":"todo"}`,
		`{"type":"/problems/not_found","title":"Not Found","status":404,"detail":"task `+logo.ID.String()+` not found","kind":"not_found"}`)

	// The tasks of a status, or of any.
	var page contract.Page[contract.Task]
	if alice.want(200, "GET", tasks+"?status=doing", "", &page); len(page.Items) != 1 || page.Items[0].ID != copyTask.ID {
		t.Errorf("tasks doing: %+v, want the copy", page.Items)
	}
	if alice.want(200, "GET", tasks, "", &page); len(page.Items) != 2 || page.Items[0].ID != copyTask.ID || page.Items[1].ID != logo.ID {
		t.Errorf("all tasks: %+v, want the copy and the logo", page.Items)
	}
	alice.problem("GET", tasks+"?status=later", "",
		`{"type":"/problems/invalid_argument","title":"Invalid Argument","status":400,"detail":"validation failed","kind":"invalid_argument",`+
			`"errors":[{"pointer":"/status","detail":"must be one of: todo, doing, done"}]}`)

	// A project with tasks stays until they're gone.
	alice.problem("DELETE", "/projects/"+web.ID.String(), "",
		`{"type":"/problems/failed_precondition","title":"Failed Precondition","status":409,"detail":"project `+web.ID.String()+` has tasks: delete them first","kind":"failed_precondition"}`)
	for _, task := range []contract.Task{logo, copyTask} {
		bob.want(404, "DELETE", "/tasks/"+task.ID.String(), "", nil)
		alice.want(204, "DELETE", "/tasks/"+task.ID.String(), "", nil)
	}
	alice.want(204, "DELETE", "/projects/"+web.ID.String(), "", nil)
}

func TestAuthorization(t *testing.T) {
	t.Parallel()
	srv := newServer(t)

	// No token: 401 with the challenge of the service.
	resp, body := caller{t: t, client: srv}.do("GET", "/projects", "")
	if resp.StatusCode != 401 || resp.Header.Get("WWW-Authenticate") != `Bearer realm="tasks"` ||
		body != `{"type":"/problems/unauthenticated","title":"Unauthenticated","status":401,"detail":"a bearer token is required","kind":"unauthenticated"}` {
		t.Errorf("GET /projects without a token = %d %v %s", resp.StatusCode, resp.Header, body)
	}

	// The stats are for admins only.
	alice := as(t, srv, "alice")
	alice.want(201, "POST", "/projects", `{"key":"WEB","name":"Website"}`, nil)
	alice.problem("GET", "/admin/stats", "",
		`{"type":"/problems/permission_denied","title":"Permission Denied","status":403,"detail":"admin.stats requires the role admin","kind":"permission_denied"}`)
	var stats contract.Stats
	if as(t, srv, "root", "admin").want(200, "GET", "/admin/stats", "", &stats); stats != (contract.Stats{Projects: 1}) {
		t.Errorf("stats = %+v, want 1 project", stats)
	}
}

// bearer is a transport that sends a token with each request, as a client
// of the service does.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

func TestJSONRPC(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	token := auth.NewToken(testKey, auth.Caller{ID: "alice"}, time.Hour)
	tasks := jsonrpc.NewClient(server+"/rpc", &http.Client{Transport: bearer{token, srv.Transport}})
	ctx := t.Context()

	// The same operations, with the types of the contract.
	web, err := tasks.Call(ctx, contract.CreateProject, contract.CreateProjectReq{Key: "WEB", Name: "Website"})
	if err != nil {
		t.Fatal(err)
	}
	logo, err := tasks.Call(ctx, contract.CreateTask, contract.CreateTaskReq{ProjectID: web.ID, Title: "Draw a logo"})
	if err != nil || logo.Number != 1 || logo.Location != "" {
		t.Fatalf("CreateTask = %+v, %v; want number 1, without the Location of REST", logo, err)
	}
	done, err := tasks.Call(ctx, contract.UpdateTask, contract.UpdateTaskReq{ID: logo.ID, Status: new("done")})
	if err != nil || done.Status != "done" {
		t.Errorf("UpdateTask = %+v, %v", done, err)
	}
	page, err := tasks.Call(ctx, contract.ListTasks, contract.ListTasksReq{ProjectID: web.ID, Status: "done"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != logo.ID {
		t.Errorf("ListTasks = %+v, %v", page, err)
	}

	// What the server answers is a ServerError, of the kind of the
	// error.
	_, err = tasks.Call(ctx, contract.DeleteProject, contract.ProjectReq{ID: web.ID})
	if se, ok := errors.AsType[*jsonrpc.ServerError](err); !ok || se.Kind != tyr.KindFailedPrecondition || se.Code != 409 {
		t.Errorf("DeleteProject of a project with tasks: %v, want failed_precondition", err)
	}
	_, err = tasks.Call(ctx, contract.GetStats, struct{}{})
	if se, ok := errors.AsType[*jsonrpc.ServerError](err); !ok || se.Kind != tyr.KindPermissionDenied {
		t.Errorf("GetStats of a caller without the role: %v, want permission_denied", err)
	}
}

func TestDocuments(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	anyone := caller{t: t, client: srv}
	var openapi struct {
		OpenAPI string         `json:"openapi"`
		Paths   map[string]any `json:"paths"`
	}
	if anyone.want(200, "GET", "/openapi.json", "", &openapi); openapi.OpenAPI != "3.1.2" || len(openapi.Paths) != 5 {
		t.Errorf("OpenAPI %s with %d paths, want 3.1.2 with 5", openapi.OpenAPI, len(openapi.Paths))
	}
	var discover struct {
		Result struct {
			Methods []struct {
				Name string `json:"name"`
			} `json:"methods"`
		} `json:"result"`
	}
	if anyone.want(200, "POST", "/rpc", `{"jsonrpc":"2.0","method":"rpc.discover","id":1}`, &discover); len(discover.Result.Methods) != 10 {
		t.Errorf("OpenRPC with %d methods, want 10", len(discover.Result.Methods))
	}
}
