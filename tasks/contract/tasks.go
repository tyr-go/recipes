package contract

import (
	"time"
	"uuid"

	"github.com/tyr-go/tyr"
)

//guide:status

// Status is where a task is: an enum, whose values the API checks in
// requests and results and the documents list.
type Status string

// The statuses of a task.
const (
	StatusTodo  Status = "todo"
	StatusDoing Status = "doing"
	StatusDone  Status = "done"
)

// EnumValues lists the statuses, as tyr.Enum has it.
func (Status) EnumValues() []Status { return []Status{StatusTodo, StatusDoing, StatusDone} }

//guide:end

// Task is a task of a project.
type Task struct {
	ID        uuid.UUID  `json:"id" doc:"The id of the task."`
	ProjectID uuid.UUID  `json:"project_id" doc:"The id of the project of the task."`
	Number    int        `json:"number" doc:"The number of the task within its project: 1 for the first one."`
	Title     string     `json:"title" doc:"What to do."`
	Status    Status     `json:"status" doc:"Where the task is."`
	DueAt     *time.Time `json:"due_at,omitzero" doc:"When the task is due, if it is."`
	CreatedAt time.Time  `json:"created_at" doc:"When the task was created."`
	UpdatedAt time.Time  `json:"updated_at" doc:"When the task last changed."`
}

// TaskCreated is a task just created, with the path of its resource,
// which REST sends as the Location of 201 Created. JSON-RPC sends only the
// task.
type TaskCreated struct {
	Task
	Location string `json:"-" header:"Location" doc:"The path of the task."`
}

//guide:create-task-req

// CreateTaskReq is a request to create a task in a project of the caller.
type CreateTaskReq struct {
	ProjectID uuid.UUID  `json:"project_id" path:"project_id" validate:"required" doc:"The id of the project."`
	Title     string     `json:"title" validate:"required,max=200" doc:"What to do."`
	Status    Status     `json:"status,omitzero" doc:"Where the task is: todo if not set."`
	DueAt     *time.Time `json:"due_at,omitzero" doc:"When the task is due."`
}

//guide:end

// TaskReq is a request for a task of a project of the caller, by its id.
type TaskReq struct {
	ID uuid.UUID `json:"id" path:"id" validate:"required" doc:"The id of the task."`
}

// ListTasksReq is a request for a page of the tasks of a project of the
// caller.
type ListTasksReq struct {
	ProjectID uuid.UUID `json:"project_id" path:"project_id" validate:"required" doc:"The id of the project."`
	Status    Status    `json:"status,omitzero" query:"status" doc:"Only the tasks of the status, if set."`
	Limit     int       `json:"limit,omitzero" query:"limit" validate:"omitempty,min=1,max=100" doc:"How many tasks to return: 20 if not set."`
	Cursor    string    `json:"cursor,omitzero" query:"cursor" doc:"The next_cursor of the page before, for the page after it."`
}

// UpdateTaskReq is a request to change a task of a project of the caller:
// the fields it sets change, and the others keep their values.
type UpdateTaskReq struct {
	ID     uuid.UUID  `json:"id" path:"id" validate:"required" doc:"The id of the task."`
	Title  *string    `json:"title,omitzero" validate:"omitempty,min=1,max=200" doc:"What to do."`
	Status *Status    `json:"status,omitzero" doc:"Where the task is."`
	DueAt  *time.Time `json:"due_at,omitzero" doc:"When the task is due."`
}

// Validate holds the rule tags can't express: an update changes
// something.
func (r UpdateTaskReq) Validate() error {
	if r.Title == nil && r.Status == nil && r.DueAt == nil {
		return tyr.InvalidArgument("nothing to change: set title, status or due_at")
	}
	return nil
}
