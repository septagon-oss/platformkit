package resource_test

// sectioned_record_edges_test.go pins the two paths of the record's blocks that
// carry an instant and that no other case reaches: the trailing list a field
// naming no section lands in, and the hidden field the record never draws. The
// value door is on the one list every block opens, so an instant outside every
// block is still an element — and a field the author kept off the screen feeds
// the door nothing, however much its value looks like an instant.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func sectioned() resource.Resource {
	return resource.Resource{
		Schema: entity.Schema{Module: "note", Entity: "note", Path: "/api/v1/note/notes",
			Fields: []entity.Field{
				{Name: "title", Type: entity.TypeString, Presentation: entity.FieldHints{Section: "basics"}},
				{Name: "created", Type: entity.TypeTime},
			}},
		Present: entity.EntryHints{Sections: []entity.EntitySection{{Key: "basics", Label: "Basics"}}},
		Screen:  "/app/note/notes",
	}
}

// A time field that names no section is drawn after every declared block, and it
// is still the same element in the same words a block-held instant would be.
func TestATimeOutsideEveryBlockStillStatesItsInstant(t *testing.T) {
	t.Parallel()
	row := map[string]any{"id": "1", "title": "Buy milk", "created": "2026-07-01T14:12:00Z"}
	out := render(t, resource.Detail(sectioned(), opts, row, false).Body)
	block := strings.Index(out, "Basics")
	instant := strings.Index(out, `<time datetime="2026-07-01T14:12:00Z"`)
	if block < 0 || instant < 0 {
		t.Fatalf("the record lost its block heading (at %d) or its trailing instant (at %d):\n%s", block, instant, out)
	}
	if instant < block {
		t.Errorf("a field naming no section was drawn before the declared blocks (time at %d, heading at %d)", instant, block)
	}
	for _, want := range []string{`title="2026-07-01 14:12:00 UTC"`, ">2026-07-01 14:12</"} {
		if !strings.Contains(out, want) {
			t.Errorf("the trailing instant lacks %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "<time"); n != 1 {
		t.Errorf("the record holds %d time elements where one field is a time:\n%s", n, out)
	}
}

// A time the author kept off every screen is off this one too: no term, no
// element, no datetime — the instant does not leak through the value door.
func TestAHiddenTimeStatesNoInstant(t *testing.T) {
	t.Parallel()
	r := sectioned()
	r.Schema.Fields[1].Presentation = entity.FieldHints{Visibility: "hidden"}
	row := map[string]any{"id": "1", "title": "Buy milk", "created": "2026-07-01T14:12:00Z"}
	out := render(t, resource.Detail(r, opts, row, false).Body)
	if strings.Contains(out, "<time") || strings.Contains(out, "2026-07-01") {
		t.Errorf("a hidden time field reached the record anyway:\n%s", out)
	}
	if !strings.Contains(out, "Buy milk") {
		t.Errorf("the shown field is gone with the hidden one:\n%s", out)
	}
}
