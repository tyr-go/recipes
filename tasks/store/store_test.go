package store_test

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/tyr-go/recipes/tasks/internal/dbtest"
	"github.com/tyr-go/recipes/tasks/pgerr"
	"github.com/tyr-go/recipes/tasks/store"
)

// createProject creates a project of the owner with the key, or fails t.
func createProject(t *testing.T, db *store.DB, owner, key string) store.Project {
	t.Helper()
	p, err := db.CreateProject(t.Context(), store.CreateProjectParams{ID: uuid.NewV7(), OwnerID: owner, Key: key, Name: "Project " + key})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// newTask creates a task in the project p, numbered in a transaction, as
// the service does.
func newTask(ctx context.Context, db *store.DB, p store.Project, title, status string) (store.Task, error) {
	var task store.Task
	err := db.InTx(ctx, func(q *store.Queries) error {
		n, err := q.NextTaskNumber(ctx, store.NextTaskNumberParams{ID: p.ID, OwnerID: p.OwnerID})
		if err != nil {
			return err
		}
		task, err = q.CreateTask(ctx, store.CreateTaskParams{ID: uuid.NewV7(), ProjectID: p.ID, Number: n, Title: title, Status: status})
		return err
	})
	return task, err
}

// createTask creates a task in the project p, or fails t.
func createTask(t *testing.T, db *store.DB, p store.Project, title, status string) store.Task {
	t.Helper()
	task, err := newTask(t.Context(), db, p, title, status)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestProjects(t *testing.T) {
	t.Parallel()
	db := store.NewDB(dbtest.New(t))
	ctx := t.Context()

	web := createProject(t, db, "alice", "WEB")
	if web.OwnerID != "alice" || web.Key != "WEB" || web.Name != "Project WEB" || web.LastNumber != 0 || web.CreatedAt.IsZero() {
		t.Errorf("created %+v", web)
	}
	// Another owner may have a project of the same key, the owner may not.
	createProject(t, db, "bob", "WEB")
	_, err := db.CreateProject(ctx, store.CreateProjectParams{ID: uuid.NewV7(), OwnerID: "alice", Key: "WEB", Name: "Again"})
	if code := pgerr.Code(err); code != pgerrcode.UniqueViolation {
		t.Errorf("a second project WEB of alice: %v (%q), want a unique violation", err, code)
	}

	// A project is its owner's only.
	got, err := db.GetProject(ctx, store.GetProjectParams{ID: web.ID, OwnerID: "alice"})
	if err != nil || got != web {
		t.Errorf("GetProject = %+v, %v; want %+v", got, err, web)
	}
	if _, err := db.GetProject(ctx, store.GetProjectParams{ID: web.ID, OwnerID: "bob"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetProject of another owner: %v, want no rows", err)
	}
	if n, err := db.DeleteProject(ctx, store.DeleteProjectParams{ID: web.ID, OwnerID: "bob"}); err != nil || n != 0 {
		t.Errorf("DeleteProject by another owner = %d, %v; want 0", n, err)
	}
	if n, err := db.DeleteProject(ctx, store.DeleteProjectParams{ID: web.ID, OwnerID: "alice"}); err != nil || n != 1 {
		t.Errorf("DeleteProject = %d, %v; want 1", n, err)
	}
}

func TestListProjects(t *testing.T) {
	t.Parallel()
	db := store.NewDB(dbtest.New(t))
	var keys []string // alice's, newest first
	for _, key := range []string{"ONE", "TWO", "THREE", "FOUR", "FIVE"} {
		createProject(t, db, "alice", key)
		keys = slices.Insert(keys, 0, key)
	}
	createProject(t, db, "bob", "BOB")

	// Pages of two, each after the last project of the one before.
	var got [][]string
	var after *uuid.UUID
	for {
		page, err := db.ListProjects(t.Context(), store.ListProjectsParams{OwnerID: "alice", After: after, Max: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		var ks []string
		for _, p := range page {
			ks = append(ks, p.Key)
		}
		got = append(got, ks)
		after = &page[len(page)-1].ID
	}
	want := [][]string{keys[0:2], keys[2:4], keys[4:5]}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("pages = %q, want %q", got, want)
	}
}

func TestTasks(t *testing.T) {
	t.Parallel()
	db := store.NewDB(dbtest.New(t))
	ctx := t.Context()
	web := createProject(t, db, "alice", "WEB")

	// Tasks are numbered within their project.
	logo := createTask(t, db, web, "Draw a logo", "todo")
	copyTask := createTask(t, db, web, "Write the copy", "doing")
	deploy := createTask(t, db, web, "Deploy", "todo")
	for i, task := range []store.Task{logo, copyTask, deploy} {
		if task.Number != int32(i+1) || task.ProjectID != web.ID || task.DueAt != nil || task.UpdatedAt.IsZero() {
			t.Errorf("task %d: %+v", i+1, task)
		}
	}
	other := createTask(t, db, createProject(t, db, "alice", "APP"), "Other", "todo")
	if other.Number != 1 {
		t.Errorf("the first task of another project is number %d, want 1", other.Number)
	}

	// A task is the owner's of its project only.
	if got, err := db.GetTask(ctx, store.GetTaskParams{ID: logo.ID, OwnerID: "alice"}); err != nil || got != logo {
		t.Errorf("GetTask = %+v, %v; want %+v", got, err, logo)
	}
	if _, err := db.GetTask(ctx, store.GetTaskParams{ID: logo.ID, OwnerID: "bob"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetTask of another owner: %v, want no rows", err)
	}

	// The tasks of a project, newest first, of a status or of any.
	list := func(status *string) []int32 {
		tasks, err := db.ListTasks(ctx, store.ListTasksParams{ProjectID: web.ID, Status: status, Max: 10})
		if err != nil {
			t.Fatal(err)
		}
		var numbers []int32
		for _, task := range tasks {
			numbers = append(numbers, task.Number)
		}
		return numbers
	}
	todo := "todo"
	if got := list(nil); !slices.Equal(got, []int32{3, 2, 1}) {
		t.Errorf("all tasks: %v, want [3 2 1]", got)
	}
	if got := list(&todo); !slices.Equal(got, []int32{3, 1}) {
		t.Errorf("tasks to do: %v, want [3 1]", got)
	}

	// An update sets what isn't nil and keeps the rest.
	done, due := "done", time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	updated, err := db.UpdateTask(ctx, store.UpdateTaskParams{ID: logo.ID, OwnerID: "alice", Status: &done, DueAt: &due})
	if err != nil || updated.Title != "Draw a logo" || updated.Status != "done" || !updated.DueAt.Equal(due) || !updated.UpdatedAt.After(logo.UpdatedAt) {
		t.Errorf("UpdateTask = %+v, %v", updated, err)
	}
	if _, err := db.UpdateTask(ctx, store.UpdateTaskParams{ID: logo.ID, OwnerID: "bob", Status: &todo}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("UpdateTask by another owner: %v, want no rows", err)
	}

	// A project with tasks can't be deleted.
	_, err = db.DeleteProject(ctx, store.DeleteProjectParams{ID: web.ID, OwnerID: "alice"})
	if code := pgerr.Code(err); code != pgerrcode.ForeignKeyViolation {
		t.Errorf("deleting a project with tasks: %v (%q), want a foreign key violation", err, code)
	}
	if n, err := db.DeleteTask(ctx, store.DeleteTaskParams{ID: deploy.ID, OwnerID: "bob"}); err != nil || n != 0 {
		t.Errorf("DeleteTask by another owner = %d, %v; want 0", n, err)
	}
	if n, err := db.DeleteTask(ctx, store.DeleteTaskParams{ID: deploy.ID, OwnerID: "alice"}); err != nil || n != 1 {
		t.Errorf("DeleteTask = %d, %v; want 1", n, err)
	}
}

func TestInTx(t *testing.T) {
	t.Parallel()
	db := store.NewDB(dbtest.New(t))
	web := createProject(t, db, "alice", "WEB")

	// A transaction that fails takes its changes with it: the number it
	// took goes to the next task.
	stop := errors.New("stop")
	err := db.InTx(t.Context(), func(q *store.Queries) error {
		if _, err := q.NextTaskNumber(t.Context(), store.NextTaskNumberParams{ID: web.ID, OwnerID: "alice"}); err != nil {
			return err
		}
		return stop
	})
	if err != stop {
		t.Errorf("InTx = %v, want the error of fn as it is", err)
	}
	if task := createTask(t, db, web, "First", "todo"); task.Number != 1 {
		t.Errorf("the number after a rollback is %d, want 1", task.Number)
	}
}

func TestConcurrentNumbers(t *testing.T) {
	t.Parallel()
	db := store.NewDB(dbtest.New(t))
	web := createProject(t, db, "alice", "WEB")

	// Tasks created at once get numbers one after another: the update of
	// the project locks its row until each transaction ends.
	const n = 20
	tasks := make([]store.Task, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			var err error
			if tasks[i], err = newTask(t.Context(), db, web, "Task", "todo"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	slices.SortFunc(tasks, func(a, b store.Task) int { return cmp.Compare(a.Number, b.Number) })
	for i, task := range tasks {
		if task.Number != int32(i+1) {
			t.Fatalf("numbers of %d tasks created at once: %d at %d, want 1 to %d", n, task.Number, i, n)
		}
	}
}

func TestMigrations(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	ctx := t.Context()

	// Every migration can be undone and done again.
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()
	p, err := goose.NewProvider(goose.DialectPostgres, db, store.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	down, err := p.DownTo(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	up, err := p.Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(down) == 0 || len(up) != len(down) {
		t.Errorf("undid %d migrations and did %d again, want all of them both ways", len(down), len(up))
	}

	// Migrate applies nothing to a database that has every migration.
	if err := store.Migrate(ctx, pool, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if current, target, err := p.GetVersions(ctx); err != nil || current != target {
		t.Errorf("versions = %d, %d, %v; want the same", current, target, err)
	}
}
