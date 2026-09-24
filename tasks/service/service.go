// Package service is the business logic of the service: a handler for
// each operation of the contract, a plain function of a request that
// returns a result, on the store.
//
// A handler translates the errors it expects into errors of tyr itself,
// with messages that say what they are about, such as a project that
// isn't found or a key that's taken. It returns the others as they are:
// pgerr.Map translates those of the database, and the rest are internal
// errors, which the client sees without their details.
package service

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"uuid"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/tyr-go/recipes/tasks/auth"
	"github.com/tyr-go/recipes/tasks/contract"
	"github.com/tyr-go/recipes/tasks/pgerr"
	"github.com/tyr-go/recipes/tasks/store"
	"github.com/tyr-go/tyr"
)

// defaultLimit is the size of a page whose request doesn't say.
const defaultLimit = 20

// Service implements the operations of the contract on a database. A
// caller owns the projects it creates and their tasks, and sees nothing
// else: the projects of others are as good as missing.
type Service struct {
	db *store.DB
}

// New returns a Service on db.
func New(db *store.DB) *Service {
	return &Service{db: db}
}

// CreateProject implements contract.CreateProject.
func (s *Service) CreateProject(ctx context.Context, req contract.CreateProjectReq) (contract.ProjectCreated, error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return contract.ProjectCreated{}, err
	}
	p, err := s.db.CreateProject(ctx, store.CreateProjectParams{ID: uuid.NewV7(), OwnerID: owner, Key: req.Key, Name: req.Name})
	if pgerr.Code(err) == pgerrcode.UniqueViolation {
		return contract.ProjectCreated{}, tyr.AlreadyExists("a project with the key %s exists", req.Key).WithCause(err)
	}
	if err != nil {
		return contract.ProjectCreated{}, err
	}
	return contract.ProjectCreated{Project: projectOf(p), Location: "/projects/" + p.ID.String()}, nil
}

// GetProject implements contract.GetProject.
func (s *Service) GetProject(ctx context.Context, req contract.ProjectReq) (contract.Project, error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return contract.Project{}, err
	}
	p, err := s.db.GetProject(ctx, store.GetProjectParams{ID: req.ID, OwnerID: owner})
	if errors.Is(err, pgx.ErrNoRows) {
		return contract.Project{}, projectNotFound(req.ID)
	}
	if err != nil {
		return contract.Project{}, err
	}
	return projectOf(p), nil
}

// ListProjects implements contract.ListProjects.
func (s *Service) ListProjects(ctx context.Context, req contract.ListProjectsReq) (contract.Page[contract.Project], error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return contract.Page[contract.Project]{}, err
	}
	after, err := decodeCursor(req.Cursor)
	if err != nil {
		return contract.Page[contract.Project]{}, err
	}
	limit := cmp.Or(req.Limit, defaultLimit)
	// One more than the page, to know if there's a next one.
	rows, err := s.db.ListProjects(ctx, store.ListProjectsParams{OwnerID: owner, After: after, Max: int32(limit + 1)})
	if err != nil {
		return contract.Page[contract.Project]{}, err
	}
	page := contract.Page[contract.Project]{Items: []contract.Project{}}
	for i, p := range rows {
		if i == limit {
			page.NextCursor = encodeCursor(rows[i-1].ID)
			break
		}
		page.Items = append(page.Items, projectOf(p))
	}
	return page, nil
}

// DeleteProject implements contract.DeleteProject.
func (s *Service) DeleteProject(ctx context.Context, req contract.ProjectReq) (struct{}, error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return struct{}{}, err
	}
	n, err := s.db.DeleteProject(ctx, store.DeleteProjectParams{ID: req.ID, OwnerID: owner})
	switch {
	case pgerr.Code(err) == pgerrcode.ForeignKeyViolation: // tasks refer to it
		return struct{}{}, tyr.FailedPrecondition("project %s has tasks: delete them first", req.ID).WithCause(err)
	case err != nil:
		return struct{}{}, err
	case n == 0:
		return struct{}{}, projectNotFound(req.ID)
	}
	return struct{}{}, nil
}

// CreateTask implements contract.CreateTask.
func (s *Service) CreateTask(ctx context.Context, req contract.CreateTaskReq) (contract.TaskCreated, error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return contract.TaskCreated{}, err
	}
	var task store.Task
	err = s.db.InTx(ctx, func(q *store.Queries) error {
		// Numbering the task locks the project until the transaction
		// ends, and finds no project unless it's the caller's.
		n, err := q.NextTaskNumber(ctx, store.NextTaskNumberParams{ID: req.ProjectID, OwnerID: owner})
		if errors.Is(err, pgx.ErrNoRows) {
			return projectNotFound(req.ProjectID)
		}
		if err != nil {
			return err
		}
		task, err = q.CreateTask(ctx, store.CreateTaskParams{
			ID:        uuid.NewV7(),
			ProjectID: req.ProjectID,
			Number:    n,
			Title:     req.Title,
			Status:    cmp.Or(req.Status, "todo"),
			DueAt:     req.DueAt,
		})
		return err
	})
	if err != nil {
		return contract.TaskCreated{}, err
	}
	return contract.TaskCreated{Task: taskOf(task), Location: "/tasks/" + task.ID.String()}, nil
}

// GetTask implements contract.GetTask.
func (s *Service) GetTask(ctx context.Context, req contract.TaskReq) (contract.Task, error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return contract.Task{}, err
	}
	t, err := s.db.GetTask(ctx, store.GetTaskParams{ID: req.ID, OwnerID: owner})
	if errors.Is(err, pgx.ErrNoRows) {
		return contract.Task{}, taskNotFound(req.ID)
	}
	if err != nil {
		return contract.Task{}, err
	}
	return taskOf(t), nil
}

// ListTasks implements contract.ListTasks.
func (s *Service) ListTasks(ctx context.Context, req contract.ListTasksReq) (contract.Page[contract.Task], error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return contract.Page[contract.Task]{}, err
	}
	after, err := decodeCursor(req.Cursor)
	if err != nil {
		return contract.Page[contract.Task]{}, err
	}
	// The project of another owner is not found, rather than empty.
	if _, err := s.db.GetProject(ctx, store.GetProjectParams{ID: req.ProjectID, OwnerID: owner}); errors.Is(err, pgx.ErrNoRows) {
		return contract.Page[contract.Task]{}, projectNotFound(req.ProjectID)
	} else if err != nil {
		return contract.Page[contract.Task]{}, err
	}
	var status *string
	if req.Status != "" {
		status = &req.Status
	}
	limit := cmp.Or(req.Limit, defaultLimit)
	rows, err := s.db.ListTasks(ctx, store.ListTasksParams{ProjectID: req.ProjectID, Status: status, After: after, Max: int32(limit + 1)})
	if err != nil {
		return contract.Page[contract.Task]{}, err
	}
	page := contract.Page[contract.Task]{Items: []contract.Task{}}
	for i, t := range rows {
		if i == limit {
			page.NextCursor = encodeCursor(rows[i-1].ID)
			break
		}
		page.Items = append(page.Items, taskOf(t))
	}
	return page, nil
}

// UpdateTask implements contract.UpdateTask.
func (s *Service) UpdateTask(ctx context.Context, req contract.UpdateTaskReq) (contract.Task, error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return contract.Task{}, err
	}
	t, err := s.db.UpdateTask(ctx, store.UpdateTaskParams{ID: req.ID, OwnerID: owner, Title: req.Title, Status: req.Status, DueAt: req.DueAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return contract.Task{}, taskNotFound(req.ID)
	}
	if err != nil {
		return contract.Task{}, err
	}
	return taskOf(t), nil
}

// DeleteTask implements contract.DeleteTask.
func (s *Service) DeleteTask(ctx context.Context, req contract.TaskReq) (struct{}, error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return struct{}{}, err
	}
	n, err := s.db.DeleteTask(ctx, store.DeleteTaskParams{ID: req.ID, OwnerID: owner})
	if err != nil {
		return struct{}{}, err
	}
	if n == 0 {
		return struct{}{}, taskNotFound(req.ID)
	}
	return struct{}{}, nil
}

// GetStats implements contract.GetStats.
func (s *Service) GetStats(ctx context.Context, _ struct{}) (contract.Stats, error) {
	c, err := s.db.CountAll(ctx)
	if err != nil {
		return contract.Stats{}, err
	}
	return contract.Stats{Projects: c.Projects, Tasks: c.Tasks, Done: c.Done}, nil
}

// ownerOf returns the id of the caller of ctx, who owns what it creates.
// Every operation of the service requires a caller, so a call without one
// is a bug of the server: it fails, rather than serve the projects of no
// one to anyone.
func ownerOf(ctx context.Context) (string, error) {
	c, ok := auth.CallerFrom(ctx)
	if !ok {
		return "", errors.New("service: a call without a caller: its operation lacks auth.Require")
	}
	return c.ID, nil
}

func projectNotFound(id uuid.UUID) error {
	return tyr.NotFound("project %s not found", id)
}

func taskNotFound(id uuid.UUID) error {
	return tyr.NotFound("task %s not found", id)
}

// projectOf returns the project of the contract for a row of the store.
func projectOf(p store.Project) contract.Project {
	return contract.Project{ID: p.ID, Key: p.Key, Name: p.Name, CreatedAt: p.CreatedAt.UTC()}
}

// taskOf returns the task of the contract for a row of the store.
func taskOf(t store.Task) contract.Task {
	task := contract.Task{
		ID:        t.ID,
		ProjectID: t.ProjectID,
		Number:    int(t.Number),
		Title:     t.Title,
		Status:    t.Status,
		CreatedAt: t.CreatedAt.UTC(),
		UpdatedAt: t.UpdatedAt.UTC(),
	}
	if t.DueAt != nil {
		task.DueAt = new(t.DueAt.UTC())
	}
	return task
}

// A cursor is the id of the last item of a page, in base64: the next page
// starts after it. Clients don't read cursors, they send back the
// next_cursor of a page for the page after it.

// encodeCursor returns the cursor of the page after the item id.
func encodeCursor(id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString(id[:])
}

// decodeCursor returns the id of cursor, or nil for no cursor, the first
// page. A string that isn't a cursor fails with a violation of the member
// cursor.
func decodeCursor(cursor string) (*uuid.UUID, error) {
	if cursor == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(b) != len(uuid.UUID{}) {
		var v tyr.Violations
		v.Add("cursor", "must be the next_cursor of a page")
		return nil, v.Err()
	}
	id := uuid.UUID(b)
	return &id, nil
}
