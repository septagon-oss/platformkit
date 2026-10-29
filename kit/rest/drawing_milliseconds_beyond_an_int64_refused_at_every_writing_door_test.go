package rest_test

import (
	"context"
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

// TestEveryDrawingDoorRefusesAPointWhoseMillisecondsDoNotFitAnInt64 asks the three doors that
// write a drawing about an elapsed time written with more digits than any int64 holds: 2^63, a
// twenty-digit count, 2^64 and 1e30 are refused at each door, as is a coordinate written the same
// way, and none of the fifteen refusals writes a byte or publishes an event. The boundary either
// side of that is pinned too: a point at exactly the last legal millisecond is accepted by the
// patch door and by the posted form, and the digits come back from the row unchanged.
//
// A door reads a drawing's digits itself rather than through a generic JSON tree, so the counts
// that do not fit an int64 are the ones a decoder could quietly clamp, wrap into a negative or
// round through a float64 — a drawing stored with a time nobody drew, readable at the revision of
// the write that invented it. A refusal that instead accepts 9223372036854775807 for a body that
// said 9223372036854775808 would keep every other case in this package green.
func TestEveryDrawingDoorRefusesAPointWhoseMillisecondsDoNotFitAnInt64(t *testing.T) {
	ceiling := strconv.FormatInt(entity.SketchMaxMilliseconds, 10)
	s := rest.Spec[*SketchRow]{Module: "drawing", Entity: "drawing", Path: "/drawings", Read: "drawing:read", Write: "drawing:write"}
	_, router, admin := mountAs(t, s, hosts{map[string]tenancy.Tenant{host: acme}}, func(api *httpx.API) {
		rest.Command(api.Surfaces(s.Module), s, "form", "Write from a posted form", "", []string{s.Event(rest.Updated)}, func(ctx context.Context, tx db.Tx[db.Tenant], rowID uuid.UUID, in struct {
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
		t.Fatalf("create the board these refusals stand on = %d %s", code, body)
	}
	at := "/api/v1/drawing/drawings/" + id(t, body)

	widerThanAnyInt64 := []struct {
		rule    string
		drawing string
	}{
		{"one past the largest int64", `[[[0,0,0],[0,0,9223372036854775808]]]`},
		{"twenty digits", `[[[0,0,0],[0,0,99999999999999999999]]]`},
		{"2^64, which a wraparound decoder reads as zero", `[[[0,0,0],[0,0,18446744073709551616]]]`},
		{"a count written in scientific notation", `[[[0,0,0],[0,0,1e30]]]`},
		{"a coordinate one past the largest int64", `[[[9223372036854775808,0,0],[0,0,0]]]`},
	}
	for _, wide := range widerThanAnyInt64 {
		for _, door := range []struct {
			name string
			post func(raw string) (int, string)
		}{
			{"the json create door", func(raw string) (int, string) {
				return call(t, router, http.MethodPost, "/api/v1/drawing/drawings", `{"title":"Wider","drawing":`+raw+`}`)
			}},
			{"the json patch door", func(raw string) (int, string) {
				return call(t, router, http.MethodPatch, at, `{"drawing":`+raw+`}`)
			}},
			{"the posted form door", func(raw string) (int, string) {
				return call(t, router, http.MethodPost, at+"/form", mustJSON(t, map[string]string{"body": url.Values{"drawing": {raw}}.Encode()}))
			}},
		} {
			code, answer := door.post(wide.drawing)
			if code != http.StatusUnprocessableEntity {
				t.Errorf("%s accepted %s (%s), want a refusal of %s", door.name, wide.drawing, answer, wide.rule)
				continue
			}
			// The refusal states a rule of the drawing and not an aggregate ceiling: a body whose
			// number does not fit is not a drawing that asks a person to undo strokes.
			if message := floorViolation(t, answer); !strings.Contains(strings.ToLower(message), "number") {
				t.Errorf("%s refused %s with %q, which states no rule about the numbers of a drawing", door.name, wide.rule, message)
			}
		}
	}

	// Fifteen refusals wrote nothing: the board still holds the drawing it was created with at the
	// revision it was created at — the largest legal count, digit for digit — and nothing was published.
	if got, revision := storedDrawingAt(t, router, at); got != drawingA || revision != 1 {
		t.Fatalf("the refusals left the drawing at %s revision %d, want the one it was created with at revision 1", got, revision)
	}
	if n := count(t, admin, s.Event(rest.Created)); n != 1 {
		t.Errorf("%d created events for one accepted create and fifteen refused ones", n)
	}
	if n := count(t, admin, s.Event(rest.Updated)); n != 0 {
		t.Errorf("%d updates for fifteen refused writes", n)
	}

	// Refusing what does not fit must not refuse what does: the last legal count passes both doors
	// that write to an existing row, and the row reads back the digits it was sent.
	last := `[[[0,0,0],[7,7,` + ceiling + `]]]`
	if code, answer := call(t, router, http.MethodPatch, at, `{"drawing":`+last+`}`); code != http.StatusOK {
		t.Fatalf("the json patch door refused a point at the last legal millisecond (%d %s)", code, answer)
	}
	if got, revision := storedDrawingAt(t, router, at); got != last || revision != 2 {
		t.Fatalf("the patch door stored %s at revision %d, want %s at revision 2", got, revision, last)
	}
	formatted := `[[[0,0,0],[3,4,` + ceiling + `]],[[1,1,0]]]`
	if code, answer := call(t, router, http.MethodPost, at+"/form", mustJSON(t, map[string]string{"body": url.Values{"drawing": {formatted}}.Encode()})); code != http.StatusOK {
		t.Fatalf("the posted form door refused a point at the last legal millisecond (%d %s)", code, answer)
	}
	if got, revision := storedDrawingAt(t, router, at); got != formatted || revision != 3 {
		t.Fatalf("the form door stored %s at revision %d, want %s at revision 3", got, revision, formatted)
	}
	if n := count(t, admin, s.Event(rest.Updated)); n != 2 {
		t.Errorf("%d updates for the two accepted writes and fifteen refused ones", n)
	}
}
