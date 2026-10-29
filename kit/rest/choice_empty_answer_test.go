package rest_test

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
)

// TestChoiceCreateThatNamesNoAnswerWritesNothing pins the answer side of the
// declaration: a create that carries no member at all is the same write as one
// that carries an empty answer, and when the declaration refuses empty both are
// refused with the declared sentence before a row or an event exists. An update
// that would take the last answer away is refused the same way, and the row the
// person left behind keeps its answer, its revision and its event count.
func TestChoiceCreateThatNamesNoAnswerWritesNothing(t *testing.T) {
	handler, admin, _ := choiceWorld(t)
	const base = "/api/v1/shapes/selections"
	var rows, outbox int
	query := func() {
		t.Helper()
		if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM rest_choice_records").Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&outbox); err != nil {
			t.Fatal(err)
		}
	}
	query()
	for _, body := range []string{`{}`, `{"shapes":[]}`, `{"shapes":null}`} {
		out := choiceRequest(handler, http.MethodPost, host, base, body, "")
		if out.Code != http.StatusUnprocessableEntity {
			t.Fatalf("create %s: %d %s", body, out.Code, out.Body)
		}
		// A create that names no member at all is refused by the schema, which is
		// what a required field answers; a create that names an empty answer is
		// refused by the declaration, whose sentence the actor must read.
		if body == `{"shapes":[]}` && !strings.Contains(out.Body.String(), "Keep a shape.") {
			t.Fatalf("create %s refused without the declared sentence: %s", body, out.Body)
		}
		query()
		if rows != 0 || outbox != 0 {
			t.Fatalf("create %s wrote: %d rows, %d events", body, rows, outbox)
		}
	}
	created := choiceRequest(handler, http.MethodPost, host, base, `{"shapes":["round"]}`, "")
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	path := base + "/" + id(t, created.Body.String())
	kept, revision, events := choiceCounts(t, admin, id(t, created.Body.String()))
	out := choiceRequest(handler, http.MethodPatch, host, path, `{"shapes":[]}`, `"1"`)
	if out.Code != http.StatusUnprocessableEntity || !strings.Contains(out.Body.String(), "Keep a shape.") {
		t.Fatalf("last removal: %d %s", out.Code, out.Body)
	}
	if strings.Contains(out.Body.String(), `"shapes":[`) {
		t.Fatalf("refusal returned a row: %s", out.Body)
	}
	if got, gotRevision, gotEvents := choiceCounts(t, admin, id(t, created.Body.String())); got != kept || gotRevision != revision || gotEvents != events {
		t.Fatalf("last removal wrote: %s/%d/%d -> %s/%d/%d", kept, revision, events, got, gotRevision, gotEvents)
	}
	var absent sql.NullString
	if err := admin.QueryRowContext(t.Context(), "SELECT shapes::text FROM rest_choice_records WHERE id=$1", id(t, created.Body.String())).Scan(&absent); err != nil {
		t.Fatal(err)
	}
	if absent.String != `["round"]` {
		t.Fatalf("stored answer changed: %s", absent.String)
	}
}
