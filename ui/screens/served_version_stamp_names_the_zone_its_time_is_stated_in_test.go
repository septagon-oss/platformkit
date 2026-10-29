package screens

// A version's line says when the write happened, and a time with no zone named is
// not a time: the panel states every version in UTC, so its line has to say which
// zone it wears. Nothing read that stamp — a version drawn at its writer's wall
// clock, or one whose line lost the marker that names the zone, left the page
// showing "15 Jan 2027, 08:00" and left a person to guess from wherever they are
// which version they are looking at. This case hands the panel two writes from two
// zones on either side of UTC and asks the page for both instants as one time.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// TestTheServedVersionStampNamesTheZoneItsTimeIsStatedIn serves a record whose two
// versions were written in zones on either side of UTC — one three hours behind, one
// five and a half ahead — and requires each version's line to carry that write's
// instant as a UTC time with the zone named. A version's At is an instant; the zone
// it arrives in belongs to whoever wrote it, and the record page is not the place
// that quietly moves the clock back to it.
func TestTheServedVersionStampNamesTheZoneItsTimeIsStatedIn(t *testing.T) {
	const at = "/app/note/notes"
	id := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	// Two writes of the same field, so both lines are drawn and neither is the
	// series' first. Each instant is stated here in the writer's zone, the way the
	// versions port hands it over: what the page owes the reader is the instant, in
	// one stated zone, whichever zone the write happened to be encoded in.
	west := time.Date(2027, 1, 15, 8, 0, 0, 0, time.FixedZone("UTC-03", -3*3600))
	east := time.Date(2027, 1, 16, 2, 15, 0, 0, time.FixedZone("UTC+05:30", 5*3600+30*60))
	series := []httpx.Version{{Version: 2, At: east, Changed: []string{"body_copy"},
		Fields: versionDocument(t, "Evening note")},
		{Version: 1, At: west, Changed: []string{"body_copy"},
			Fields: versionDocument(t, "Morning note")}}

	res := httpx.Resource{
		Module: "note", Entity: "note", Path: "/notes", Screen: at,
		Schema: entity.Schema{Module: "note", Entity: "note", Path: "/notes",
			Fields: []entity.Field{{Name: "body_copy", Type: entity.TypeString}}},
		History: &httpx.History{RestoreVerb: "restore",
			Versions: func(context.Context, uuid.UUID) ([]httpx.Version, error) {
				return series, nil
			}},
	}
	row := map[string]any{"id": id.String(), "body_copy": "Evening note",
		entity.RevisionField: float64(2)}
	page, err := detailView(res, context.Background(),
		Options{Workspace: "/app", Home: "Workspace"}, at, row, true)
	if err != nil {
		t.Fatalf("the record page of a versioned resource would not render: %v", err)
	}
	var served strings.Builder
	for _, n := range page.Body {
		if err := n.Render(&served); err != nil {
			t.Fatalf("render the record page: %v", err)
		}
	}
	doc := served.String()

	// Each version's own line, at the instant it was written: a stamp that kept the
	// writer's wall clock, or that dropped the zone marker, names no time a reader
	// can place.
	for _, want := range []string{"v1 · 15 Jan 2027, 11:00 UTC", "v2 · 15 Jan 2027, 20:45 UTC"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the panel drew no version stamped %q, the same instant in the zone the panel states its times in:\n%s", want, doc)
		}
	}
	// And the writer's zones stay where they were written: the day and the wall clock
	// of the zone a version arrived in never reach the line beside that record.
	for _, absent := range []string{"15 Jan 2027, 08:00", "16 Jan 2027, 02:15"} {
		if strings.Contains(doc, absent) {
			t.Errorf("the panel drew a version at %q, a writer's wall clock rather than the instant the page states:\n%s", absent, doc)
		}
	}
}

// versionDocument is what the versions port hands over for one version of a note.
func versionDocument(t *testing.T, body string) json.RawMessage {
	t.Helper()
	fields, err := json.Marshal(map[string]string{"body_copy": body})
	if err != nil {
		t.Fatalf("encode the document a version held: %v", err)
	}
	return fields
}
