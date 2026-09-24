package service

import (
	"errors"
	"slices"
	"testing"
	"uuid"

	"github.com/tyr-go/tyr"
)

func TestCursor(t *testing.T) {
	id := uuid.NewV7()
	if got, err := decodeCursor(encodeCursor(id)); err != nil || *got != id {
		t.Errorf("the cursor of %s decodes into %v, %v", id, got, err)
	}
	if got, err := decodeCursor(""); got != nil || err != nil {
		t.Errorf("no cursor decodes into %v, %v; want none, the first page", got, err)
	}

	want := tyr.Violations{{Pointer: "/cursor", Detail: "must be the next_cursor of a page"}}
	for _, cursor := range []string{"nope", encodeCursor(id)[1:], encodeCursor(id) + "AA", "+/+/+/+/+/+/+/+/+/+/+/"} {
		_, err := decodeCursor(cursor)
		if e, ok := errors.AsType[*tyr.Error](err); !ok || e.Kind != tyr.KindInvalidArgument || !slices.Equal(e.Details.(tyr.Violations), want) {
			t.Errorf("cursor %q: %v, want %v", cursor, err, want)
		}
	}
}
