package rest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// TestTheFormDoorRefusesAFoldedCommandOwnedName (review T-0020, finding 1). The
// README says every write door refuses a key that folds onto a field a command
// owns; Values is the door ui/screens mounts for a create form, and it refuses
// only the exact spelling, so "STATUS" is neither refused nor applied and the
// caller is told nothing. The exact spelling keeps refusing either way.
func TestTheFormDoorRefusesAFoldedCommandOwnedName(t *testing.T) {
	fields := crud.Fields[*Task]()
	if _, err := rest.Values([]byte("title=Exact&status=done"), fields, []string{"status"}); err == nil {
		t.Error(`Values posted "status=done" was accepted, want the refusal this door gives today`)
	}
	if _, err := rest.Values([]byte("title=Folded&STATUS=done"), fields, []string{"status"}); err == nil {
		t.Error(`Values posted "STATUS=done" was accepted, want 422 naming "status"`)
	}
}
