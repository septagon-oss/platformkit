package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestAReadOnlyResourceDrawsNoWriteDoorEvenForAWriter asks the list and the record of a
// resource that mounts only list and read, on behalf of a caller who holds every write
// grant: the grant is who is asking, the operations are what the resource mounted, and a
// door to a form that answers 404 is the lie the generated routes no longer tell.
func TestAReadOnlyResourceDrawsNoWriteDoorEvenForAWriter(t *testing.T) {
	t.Parallel()
	row := map[string]any{"id": "1", "title": "Buy milk", "status": "open"}
	reads := note()
	reads.Operations = []string{"list", "read"}

	list := render(t, resource.List(reads, opts, []map[string]any{row}, 1, 1, "", true).Body)
	if !strings.Contains(list, `href="/app/note/notes/1"`) {
		t.Fatalf("the list does not link the record it was given:\n%s", list)
	}
	if strings.Contains(list, "/notes/new") {
		t.Fatalf("a read-only list draws a New door for a writer:\n%s", list)
	}
	record := render(t, resource.Detail(reads, opts, row, true).Body)
	if !strings.Contains(record, "Buy milk") {
		t.Fatalf("the record does not show its row:\n%s", record)
	}
	if strings.Contains(record, "/edit") {
		t.Fatalf("a read-only record draws an Edit door for a writer:\n%s", record)
	}
}
