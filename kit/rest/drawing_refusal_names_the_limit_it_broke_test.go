package rest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// drawingCeilings are the four ceilings the kernel declares on a drawing, each with the
// word a person is meant to read when the body they sent breaks it.
var drawingCeilings = map[string]struct {
	value int64
	word  string
}{
	"strokes":    {entity.SketchMaxStrokes, "stroke"},
	"points":     {entity.SketchMaxPoints, "point"},
	"coordinate": {entity.SketchMaxCoordinate, "coordinate"},
	"millis":     {entity.SketchMaxMilliseconds, "millisecond"},
}

// TestEveryDrawingDoorNamesTheLimitItRefused checks the words a person is handed when a
// drawing is refused: each of the three doors that writes a drawing answers the smallest body
// that breaks each of the four declared ceilings with the sentence for the ceiling that body
// broke, naming it and its value, and no row moves.
//
// The status code and the field name cannot tell the four ceilings apart, and no case in this
// package reads the words an answer carries: a door that answered "invalid drawing" for all four
// ceilings, or quoted the point ceiling's sentence for a drawing carrying one stroke too many,
// would leave them green while a person is told to undo strokes they never drew. The sentence is
// the value's own, so this is the half about whether a door hands it over.
func TestEveryDrawingDoorNamesTheLimitItRefused(t *testing.T) {
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
	code, body := call(t, router, http.MethodPost, "/api/v1/drawing/drawings", `{"title":"Words","drawing":`+drawingA+`}`)
	if code != http.StatusCreated {
		t.Fatalf("create the row the refusals stand on = %d %s", code, body)
	}
	at := "/api/v1/drawing/drawings/" + id(t, body)

	// Each ceiling, broken by the least a body can break it: one stroke over leaves the point
	// count untouched, and one point over leaves the stroke count well inside its own ceiling.
	refusals := []struct {
		ceiling string
		drawing string
	}{
		{"strokes", square(entity.SketchMaxStrokes+1, entity.SketchMaxStrokes+1)},
		{"points", square(1, entity.SketchMaxPoints+1)},
		{"coordinate", "[[[0,0,0],[1001,0,1]]]"},
		{"millis", fmt.Sprintf("[[[0,0,0],[0,0,%d]]]", entity.SketchMaxMilliseconds+1)},
	}
	doors := []struct {
		name string
		post func(raw string) (int, string)
	}{
		{"the json create door", func(raw string) (int, string) {
			return call(t, router, http.MethodPost, "/api/v1/drawing/drawings", `{"title":"Over","drawing":`+raw+`}`)
		}},
		{"the json patch door", func(raw string) (int, string) {
			return call(t, router, http.MethodPatch, at, `{"drawing":`+raw+`}`)
		}},
		{"the posted form door", func(raw string) (int, string) {
			return call(t, router, http.MethodPost, at+"/form", mustJSON(t, map[string]string{"body": url.Values{"drawing": {raw}}.Encode()}))
		}},
	}

	for _, d := range doors {
		for _, r := range refusals {
			code, answer := d.post(r.drawing)
			if code != http.StatusUnprocessableEntity {
				t.Errorf("%s = %d %s, want the drawing refused for one %s over its ceiling", d.name, code, answer, r.ceiling)
				continue
			}
			message := drawingRefusalMessage(t, answer)
			if message == "" {
				continue
			}
			ceiling := drawingCeilings[r.ceiling]
			if !strings.Contains(strings.ToLower(message), ceiling.word) {
				t.Errorf("%s refused one %s over with %q, want the sentence naming %s", d.name, r.ceiling, message, ceiling.word)
			}
			// The sentence states the ceiling it broke and no other: a message that quotes a
			// second ceiling has stopped answering the body that arrived.
			for name, other := range drawingCeilings {
				states := statesNumber(message, other.value)
				if states && name != r.ceiling {
					t.Errorf("%s refused one %s over with %q, which names the %s ceiling's value %d rather than the one it broke", d.name, r.ceiling, message, name, other.value)
				}
				if !states && name == r.ceiling {
					t.Errorf("%s refused one %s over with %q, which never states the %s ceiling's value %d", d.name, r.ceiling, message, name, other.value)
				}
			}
		}
	}

	// The twelve refusals wrote nothing: the row still holds the drawing it was created with.
	code, answer := call(t, router, http.MethodGet, at, "")
	var row struct {
		Drawing  json.RawMessage `json:"drawing"`
		Revision int64           `json:"revision"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(answer), &row) != nil || string(row.Drawing) != drawingA || row.Revision != 1 {
		t.Fatalf("the refusals left the row at %d %s, want the drawing it was created with at revision 1", code, answer)
	}
}

// drawingRefusalMessage reads the answer's own violation for the drawing field: each violation
// states where the fault is and the message the person reads. An answer carrying no such
// violation is recorded and yields the empty string, so the other doors are judged too rather
// than abandoned to a failure this one already reported.
func drawingRefusalMessage(t *testing.T, answer string) string {
	t.Helper()
	var problem struct {
		Violations []struct {
			Location string `json:"location"`
			Message  string `json:"message"`
		} `json:"violations"`
	}
	if err := json.Unmarshal([]byte(answer), &problem); err != nil {
		t.Errorf("the refusal is not a problem: %v (%s)", err, answer)
		return ""
	}
	for _, v := range problem.Violations {
		if strings.Contains(strings.ToLower(v.Location+v.Message), "drawing") {
			return v.Message
		}
	}
	t.Errorf("the refusal names no violation for the drawing: %s", answer)
	return ""
}

// statesNumber — whether a sentence states n, written plain or in thousands groups, so the
// sentence may be punctuated for a person rather than for the case that reads it.
func statesNumber(sentence string, n int64) bool {
	digits := strconv.FormatInt(n, 10)
	if strings.Contains(sentence, digits) {
		return true
	}
	var grouped strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(r)
	}
	return strings.Contains(sentence, grouped.String())
}
