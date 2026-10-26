package rest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// storedDrawingAt is the board's drawing and revision as the row itself answers: the
// two things a refusal must leave alone.
func storedDrawingAt(t *testing.T, router http.Handler, at string) (string, int64) {
	t.Helper()
	code, body := call(t, router, http.MethodGet, at, "")
	var got struct {
		Drawing  json.RawMessage `json:"drawing"`
		Revision int64           `json:"revision"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &got) != nil {
		t.Fatalf("the row answered %d %s", code, body)
	}
	return string(got.Drawing), got.Revision
}

// floorViolation is the one sentence a refusal hands over, and where it says the fault is.
// A body here breaks exactly one rule, so every violation the answer carries states the same
// message; the places it names are recorded so the shape of the answer is visible to a reader.
func floorViolation(t *testing.T, answer string) string {
	t.Helper()
	var problem struct {
		Violations []struct {
			Location string `json:"location"`
			Message  string `json:"message"`
		} `json:"violations"`
	}
	if err := json.Unmarshal([]byte(answer), &problem); err != nil {
		t.Fatalf("the refusal is not a problem: %v (%s)", err, answer)
	}
	if len(problem.Violations) == 0 {
		t.Fatalf("the refusal carries no violation at all: %s", answer)
	}
	var locations []string
	message := problem.Violations[0].Message
	for _, v := range problem.Violations {
		if v.Message != message {
			t.Fatalf("the refusal states two rules (%q and %q) for the one rule it broke: %s", message, v.Message, answer)
		}
		locations = append(locations, v.Location)
	}
	t.Logf("the refusal locates the fault at %s", strings.Join(locations, ", "))
	return message
}

// TestEveryDrawingDoorRefusesTheFloorOfTheSquare asks the drawing doors about the lower
// bounds of the value rather than its ceilings: a body below the square — a negative
// coordinate, a stroke that does not start at zero milliseconds, a stroke whose times run
// backwards, a stroke with no point in it — is refused at each of the three doors that
// writes one with the sentence for the rule it broke, and nothing is written or published.
//
// The square's upper edge and the two aggregate ceilings are pinned at these doors; the
// floors are pinned only in the value's own test, which no door reaches. A door that
// decoded straight into the column would store a coordinate of -1 as a drawing, and a
// drawing outside the 0–1000 square is a value the renderer, the phone and the round trip
// were all promised would never arrive.
func TestEveryDrawingDoorRefusesTheFloorOfTheSquare(t *testing.T) {
	s := rest.Spec[*SketchRow]{Module: "drawing", Entity: "drawing", Path: "/drawings", Read: "drawing:read", Write: "drawing:write"}
	_, router, admin := mountAs(t, s, hosts{map[string]tenancy.Tenant{host: acme}}, func(api *httpx.API) {
		rest.Command(api.Surfaces(s.Module), s, "form", "Write from a posted form", "", nil, func(ctx context.Context, tx db.Tx[db.Tenant], rowID uuid.UUID, in struct {
			Body string `json:"body"`
		}) (*SketchRow, error) {
			sent, err := rest.UpdateValues([]byte(in.Body), crud.Fields[*SketchRow](), nil)
			if err != nil {
				return nil, err
			}
			return s.UpdateRow(ctx, tx, rowID, sent)
		}, rest.CommandOptions{})
	})
	if _, err := admin.ExecContext(t.Context(), sketchDDL); err != nil {
		t.Fatal(err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/drawing/drawings", `{"title":"Board","drawing":`+drawingA+`}`)
	if code != http.StatusCreated {
		t.Fatalf("create the board the refusals stand on = %d %s", code, body)
	}
	at := "/api/v1/drawing/drawings/" + id(t, body)

	floors := []struct {
		rule    string
		drawing string
		word    string
	}{
		{"a coordinate below the square", `[[[-1,0,0],[5,5,1]]]`, "coordinate"},
		{"a stroke that starts away from zero", `[[[0,0,1],[5,5,2]]]`, "millisecond"},
		{"a stroke whose time runs backwards", `[[[0,0,0],[5,5,2],[6,6,1]]]`, "millisecond"},
		{"a stroke with no point in it", `[[]]`, "point"},
	}
	doors := []struct {
		name string
		post func(raw string) (int, string)
	}{
		{"the json create door", func(raw string) (int, string) {
			return call(t, router, http.MethodPost, "/api/v1/drawing/drawings", `{"title":"Below","drawing":`+raw+`}`)
		}},
		{"the json patch door", func(raw string) (int, string) {
			return call(t, router, http.MethodPatch, at, `{"drawing":`+raw+`}`)
		}},
		{"the posted form door", func(raw string) (int, string) {
			return call(t, router, http.MethodPost, at+"/form", mustJSON(t, map[string]string{"body": url.Values{"drawing": {raw}}.Encode()}))
		}},
	}

	for _, d := range doors {
		for _, f := range floors {
			code, answer := d.post(f.drawing)
			if code != http.StatusUnprocessableEntity {
				t.Errorf("%s accepted %s (%s), want %s refused as %s", d.name, f.drawing, answer, f.rule, f.word)
				continue
			}
			message := floorViolation(t, answer)
			if !strings.Contains(strings.ToLower(message), f.word) {
				t.Errorf("%s refused %s with %q, want the sentence naming %s", d.name, f.rule, message, f.word)
			}
			// A floor refusal answered with a ceiling's sentence would tell a person to undo
			// strokes they never drew: the two ceilings' sentences share one phrase.
			if strings.Contains(message, "at most") {
				t.Errorf("%s refused %s with the ceiling's sentence %q", d.name, f.rule, message)
			}
		}
	}

	// Twelve refusals wrote nothing: the board still holds the drawing it was created with
	// at the revision it was created at, and the outbox holds the one create and no update.
	if got, revision := storedDrawingAt(t, router, at); got != drawingA || revision != 1 {
		t.Fatalf("the refusals left the drawing at %s revision %d, want the one it was created with at revision 1", got, revision)
	}
	if n := count(t, admin, s.Event(rest.Created)); n != 1 {
		t.Errorf("%d created events for one accepted create and four refused ones", n)
	}
	if n := count(t, admin, s.Event(rest.Updated)); n != 0 {
		t.Errorf("%d updates for eight refused writes", n)
	}
}
