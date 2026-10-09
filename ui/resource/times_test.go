package resource_test

// times_test.go pins what a generated screen does with an instant. The words in
// the cell are still the ones this kernel has always written — the zone a person
// reads is chosen by the reader's own engine (ui/assets/js/times.js), and putting
// a zone into the shared string instead would have moved every reading of it,
// including everything kit/rest delegates. What is new is the machine-readable
// instant beside those words, and the exact moment in the title.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func timed() resource.Resource {
	return resource.Resource{
		Schema: entity.Schema{Module: "note", Entity: "note", Path: "/api/v1/note/notes",
			Fields: []entity.Field{
				{Name: "title", Type: entity.TypeString},
				{Name: "created", Type: entity.TypeTime},
				{Name: "exported", Type: entity.TypeTime},
				{Name: "reviewed", Type: entity.TypeString},
			}},
		Screen: "/app/note/notes",
	}
}

func TestAListCellStatesAnInstantTheReaderCanTranslate(t *testing.T) {
	t.Parallel()
	row := map[string]any{"id": "1", "title": "Buy milk", "created": "2026-07-01T14:12:00Z",
		"exported": "not a date", "reviewed": "2026-07-01T14:12:00Z"}
	out := render(t, resource.List(timed(), opts, []map[string]any{row}, 1, 1, "", false).Body)
	if !strings.Contains(out, `<time datetime="2026-07-01T14:12:00Z"`) {
		t.Errorf("the time cell carries no instant:\n%s", out)
	}
	if !strings.Contains(out, `title="2026-07-01 14:12:00 UTC"`) {
		t.Errorf("the time cell states no exact moment:\n%s", out)
	}
	if !strings.Contains(out, `>2026-07-01 14:12</`) {
		t.Errorf("the words in the cell moved: they are the ones every screen has always said:\n%s", out)
	}
	// A time field holding something that is no instant keeps its raw text and gains
	// no element and no datetime: an invented date is a fiction a person cannot tell
	// from the real one.
	if !strings.Contains(out, "not a date") {
		t.Errorf("a value that is no instant disappeared:\n%s", out)
	}
	if strings.Contains(out, `<time datetime="not a date"`) {
		t.Errorf("a value that is no instant was given an instant:\n%s", out)
	}
	// A string field that merely looks like an instant is a string: only a field the
	// schema calls a time becomes an element.
	if strings.Count(out, "<time") != 1 {
		t.Errorf("the markup holds %d time elements where one field is a time:\n%s", strings.Count(out, "<time"), out)
	}
}

func TestARecordStatesTheSameInstantTheListDoes(t *testing.T) {
	t.Parallel()
	row := map[string]any{"id": "1", "title": "Buy milk", "created": "2026-07-01T14:12:00Z",
		"exported": "not a date", "reviewed": "2026-07-01T14:12:00Z"}
	out := render(t, resource.Detail(timed(), opts, row, true).Body)
	for _, want := range []string{`<time datetime="2026-07-01T14:12:00Z"`, `title="2026-07-01 14:12:00 UTC"`, ">2026-07-01 14:12</"} {
		if !strings.Contains(out, want) {
			t.Errorf("the record lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "<time") != 1 {
		t.Errorf("the record holds %d time elements where one field is a time:\n%s", strings.Count(out, "<time"), out)
	}
	if !strings.Contains(out, "not a date") {
		t.Errorf("the record hid a value that is no instant instead of showing it:\n%s", out)
	}
}

// TestATimeWithoutAValueIsStillNothing checks the case that has no instant to
// carry: an absent time is a dash, not an empty element with an empty attribute.
func TestATimeWithoutAValueIsStillNothing(t *testing.T) {
	t.Parallel()
	row := map[string]any{"id": "1", "title": "Buy milk"}
	out := render(t, resource.Detail(timed(), opts, row, false).Body)
	if strings.Contains(out, "<time") {
		t.Errorf("a field with nothing in it drew an instant out of nothing:\n%s", out)
	}
}
