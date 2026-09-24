package contract_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tyr-go/recipes/tasks/contract"
	"github.com/tyr-go/tyr"
)

func TestCreateProjectReqValidate(t *testing.T) {
	invalid := tyr.Violations{{Pointer: "/key", Detail: "must be of A-Z and 0-9, starting with a letter"}}
	tests := []struct {
		key  string
		want tyr.Violations // nil if the key is valid
	}{
		{"WEB", nil},
		{"A1", nil},
		{"Q4PLAN2026", nil},
		{"web", invalid},
		{"1WEB", invalid},
		{"WE B", invalid},
		{"WEB-1", invalid},
	}
	for _, tt := range tests {
		err := contract.CreateProjectReq{Key: tt.key, Name: "Website"}.Validate()
		if tt.want == nil {
			if err != nil {
				t.Errorf("key %q: %v, want valid", tt.key, err)
			}
			continue
		}
		e, ok := errors.AsType[*tyr.Error](err)
		if !ok || e.Kind != tyr.KindInvalidArgument || !slices.Equal(e.Details.(tyr.Violations), tt.want) {
			t.Errorf("key %q: %v, want %v", tt.key, err, tt.want)
		}
	}
}

func TestUpdateTaskReqValidate(t *testing.T) {
	due := time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)
	for _, req := range []contract.UpdateTaskReq{
		{Title: new("Draw a logo")},
		{Status: new(contract.StatusDone)},
		{DueAt: &due},
	} {
		if err := req.Validate(); err != nil {
			t.Errorf("Validate of %+v: %v, want valid", req, err)
		}
	}
	err := contract.UpdateTaskReq{}.Validate()
	if e, ok := errors.AsType[*tyr.Error](err); !ok || e.Kind != tyr.KindInvalidArgument || e.Message != "nothing to change: set title, status or due_at" {
		t.Errorf("Validate of an update of nothing: %v, want invalid_argument", err)
	}
}
