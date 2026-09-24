package contract

import (
	"regexp"
	"time"
	"uuid"

	"github.com/tyr-go/tyr"
)

// Project is a project of its owner, the caller who created it.
type Project struct {
	ID        uuid.UUID `json:"id" doc:"The id of the project."`
	Key       string    `json:"key" doc:"A short code of the project, unique among the projects of its owner, such as WEB."`
	Name      string    `json:"name" doc:"The name of the project."`
	CreatedAt time.Time `json:"created_at" doc:"When the project was created."`
}

// ProjectCreated is a project just created, with the path of its
// resource, which REST sends as the Location of 201 Created. JSON-RPC
// sends only the project.
type ProjectCreated struct {
	Project
	Location string `json:"-" header:"Location" doc:"The path of the project."`
}

// CreateProjectReq is a request to create a project.
type CreateProjectReq struct {
	Key  string `json:"key" validate:"required,min=2,max=10" doc:"A short code of the project, of A-Z and 0-9, starting with a letter, such as WEB. The projects of a caller have keys of their own."`
	Name string `json:"name" validate:"required,max=100" doc:"The name of the project."`
}

var keyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]*$`)

// Validate holds the rule tags can't express: the characters of the key.
func (r CreateProjectReq) Validate() error {
	var v tyr.Violations
	if !keyRe.MatchString(r.Key) {
		v.Add("key", "must be of A-Z and 0-9, starting with a letter")
	}
	return v.Err()
}

// ProjectReq is a request for a project of the caller, by its id.
type ProjectReq struct {
	ID uuid.UUID `json:"id" path:"id" validate:"required" doc:"The id of the project."`
}

// ListProjectsReq is a request for a page of the projects of the caller.
type ListProjectsReq struct {
	Limit  int    `json:"limit,omitzero" query:"limit" validate:"omitempty,min=1,max=100" doc:"How many projects to return: 20 if not set."`
	Cursor string `json:"cursor,omitzero" query:"cursor" doc:"The next_cursor of the page before, for the page after it."`
}
