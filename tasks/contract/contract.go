// Package contract is the contract of the service: its operations, with
// their requests and results, which the server implements and its clients
// call, and their documentation, which the OpenAPI and OpenRPC documents
// of the service show. The compiler checks both sides against it, the
// examples too:
//
//	tasks := jsonrpc.NewClient("http://tasks.internal/rpc", hc) // hc sends the token
//	page, err := tasks.Call(ctx, contract.ListTasks, contract.ListTasksReq{ProjectID: id, Status: "todo"})
//
// It imports tyr and rest, for the routes of the operations, and none of
// the packages of the server. Who may call what is the server's business:
// every operation needs a caller with a token, and admin.stats one with
// the role admin.
package contract

import (
	"net/http"
	"time"
	"uuid"

	"github.com/tyr-go/tyr"
	"github.com/tyr-go/tyr/rest"
)

// The operations of the service.
var (
	CreateProject = tyr.Define[CreateProjectReq, ProjectCreated]("projects.create",
		rest.Route("POST /projects"), rest.Status(http.StatusCreated),
		tyr.Summary("Create a project"),
		tyr.Tags("projects"),
		tyr.Errors(tyr.KindAlreadyExists),
	).Example("website",
		CreateProjectReq{Key: "WEB", Name: "Website"},
		ProjectCreated{Project: website, Location: "/projects/" + website.ID.String()},
	)

	GetProject = tyr.Define[ProjectReq, Project]("projects.get",
		rest.Route("GET /projects/{id}"),
		tyr.Summary("Get a project"),
		tyr.Tags("projects"),
		tyr.Errors(tyr.KindNotFound),
	).Example("website", ProjectReq{ID: website.ID}, website)

	ListProjects = tyr.Define[ListProjectsReq, Page[Project]]("projects.list",
		rest.Route("GET /projects"),
		tyr.Summary("List the projects of the caller"),
		tyr.Description("Newest first, a page at a time."),
		tyr.Tags("projects"),
	).Example("the first page", ListProjectsReq{Limit: 2}, Page[Project]{Items: []Project{website}})

	DeleteProject = tyr.Define[ProjectReq, struct{}]("projects.delete",
		rest.Route("DELETE /projects/{id}"),
		tyr.Summary("Delete a project"),
		tyr.Description("A project with tasks can't be deleted: delete its tasks first."),
		tyr.Tags("projects"),
		tyr.Errors(tyr.KindNotFound, tyr.KindFailedPrecondition),
	)

	CreateTask = tyr.Define[CreateTaskReq, TaskCreated]("tasks.create",
		rest.Route("POST /projects/{project_id}/tasks"), rest.Status(http.StatusCreated),
		tyr.Summary("Create a task in a project"),
		tyr.Description("The task gets the next number of its project: 1, 2, and so on."),
		tyr.Tags("tasks"),
		tyr.Errors(tyr.KindNotFound),
	).Example("a logo",
		CreateTaskReq{ProjectID: website.ID, Title: "Draw a logo", DueAt: &due},
		TaskCreated{Task: logo, Location: "/tasks/" + logo.ID.String()},
	)

	GetTask = tyr.Define[TaskReq, Task]("tasks.get",
		rest.Route("GET /tasks/{id}"),
		tyr.Summary("Get a task"),
		tyr.Tags("tasks"),
		tyr.Errors(tyr.KindNotFound),
	).Example("a logo", TaskReq{ID: logo.ID}, logo)

	ListTasks = tyr.Define[ListTasksReq, Page[Task]]("tasks.list",
		rest.Route("GET /projects/{project_id}/tasks"),
		tyr.Summary("List the tasks of a project"),
		tyr.Description("Newest first, a page at a time, of a status or of any."),
		tyr.Tags("tasks"),
		tyr.Errors(tyr.KindNotFound),
	).Example("to do", ListTasksReq{ProjectID: website.ID, Status: "todo"}, Page[Task]{Items: []Task{logo}})

	UpdateTask = tyr.Define[UpdateTaskReq, Task]("tasks.update",
		rest.Route("PATCH /tasks/{id}"),
		tyr.Summary("Change a task"),
		tyr.Description("The fields of the request change those of the task, and the fields it leaves out keep their values."),
		tyr.Tags("tasks"),
		tyr.Errors(tyr.KindNotFound),
	).Example("done", UpdateTaskReq{ID: logo.ID, Status: new("done")}, logoDone)

	DeleteTask = tyr.Define[TaskReq, struct{}]("tasks.delete",
		rest.Route("DELETE /tasks/{id}"),
		tyr.Summary("Delete a task"),
		tyr.Tags("tasks"),
		tyr.Errors(tyr.KindNotFound),
	)

	GetStats = tyr.Define[struct{}, Stats]("admin.stats",
		rest.Route("GET /admin/stats"),
		tyr.Summary("Count the projects and tasks of all owners"),
		tyr.Tags("admin"),
	).Example("a day", struct{}{}, Stats{Projects: 12, Tasks: 340, Done: 205})
)

// Page is a page of a list, newest first. The next page starts after the
// last item of this one: send its NextCursor as the cursor of the request.
type Page[T any] struct {
	Items      []T    `json:"items" doc:"The items of the page, newest first."`
	NextCursor string `json:"next_cursor,omitzero" doc:"The cursor of the next page; none after the last page."`
}

// Stats are the numbers of the projects and tasks of all owners.
type Stats struct {
	Projects int64 `json:"projects" doc:"The number of projects."`
	Tasks    int64 `json:"tasks" doc:"The number of tasks."`
	Done     int64 `json:"done" doc:"The number of tasks done."`
}

// The examples of the documents.
var (
	created = time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	due     = time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)
	website = Project{
		ID:        uuid.MustParse("01997aa0-4c80-7a3e-9d21-5b6f0c1e2a3b"),
		Key:       "WEB",
		Name:      "Website",
		CreatedAt: created,
	}
	logo = Task{
		ID:        uuid.MustParse("01997aa3-1d20-7f41-8c55-7e2d9b0a4c6d"),
		ProjectID: website.ID,
		Number:    1,
		Title:     "Draw a logo",
		Status:    "todo",
		DueAt:     &due,
		CreatedAt: created.Add(3 * time.Minute),
		UpdatedAt: created.Add(3 * time.Minute),
	}
	logoDone = func() Task {
		t := logo
		t.Status = "done"
		t.UpdatedAt = created.Add(2 * time.Hour)
		return t
	}()
)
