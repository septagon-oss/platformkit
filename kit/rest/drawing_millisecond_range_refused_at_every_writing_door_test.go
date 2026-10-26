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

// TestEveryDrawingDoorRefusesEveryMillisecondCountOutsideTheRange asks the three doors that write a
// drawing about the elapsed time of a point: a point one millisecond past the ceiling and a point below
// zero are refused at each door with the sentence for that range and the ceiling's own number in it, and
// nothing is written or published. The row these refusals stand on is created with a drawing that ends a
// stroke at exactly the ceiling, so the refusals sit outside the range rather than at its edge.
//
// Elapsed time is bounded by the largest integer a JSON number keeps exactly in the browser and on the
// phone, so a count past it can reach a reader as a different number than the one written, and a stored
// drawing would no longer equal the drawing its author sent. A point below zero also breaks the rule that
// a stroke starts at zero and keeps its times in order, so a door can refuse it with that rule's sentence:
// the two sentences ask for different repairs, and only the range's names the bound a person exceeded.
func TestEveryDrawingDoorRefusesEveryMillisecondCountOutsideTheRange(t *testing.T) {
	ceiling := strconv.FormatInt(entity.SketchMaxMilliseconds, 10)
	pastCeiling := strconv.FormatInt(entity.SketchMaxMilliseconds+1, 10)
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
		t.Fatalf("create the board the refusals stand on = %d %s", code, body)
	}
	at := "/api/v1/drawing/drawings/" + id(t, body)

	// drawingA ends a stroke at exactly the ceiling, so the row these refusals stand on already answers
	// with the largest count the value allows, digit for digit.
	if got, revision := storedDrawingAt(t, router, at); got != drawingA || revision != 1 {
		t.Fatalf("the board created at the ceiling answers %s at revision %d, want %s at revision 1", got, revision, drawingA)
	}

	patch := func(raw string) (int, string) {
		return call(t, router, http.MethodPatch, at, `{"drawing":`+raw+`}`)
	}
	form := func(raw string) (int, string) {
		return call(t, router, http.MethodPost, at+"/form", mustJSON(t, map[string]string{"body": url.Values{"drawing": {raw}}.Encode()}))
	}
	outside := []struct {
		rule    string
		drawing string
	}{
		{"a point past the last millisecond", `[[[0,0,0],[0,0,` + pastCeiling + `]]]`},
		{"a point below zero milliseconds", `[[[0,0,0],[5,5,-1]]]`},
	}
	for _, door := range []struct {
		name string
		post func(raw string) (int, string)
	}{
		{"the json create door", func(raw string) (int, string) {
			return call(t, router, http.MethodPost, "/api/v1/drawing/drawings", `{"title":"Outside","drawing":`+raw+`}`)
		}},
		{"the json patch door", patch},
		{"the posted form door", form},
	} {
		for _, o := range outside {
			code, answer := door.post(o.drawing)
			if code != http.StatusUnprocessableEntity {
				t.Errorf("%s accepted %s (%s), want %s refused", door.name, o.drawing, answer, o.rule)
				continue
			}
			message := floorViolation(t, answer)
			if !strings.Contains(strings.ToLower(message), "millisecond") {
				t.Errorf("%s refused %s with %q, want the sentence naming milliseconds", door.name, o.rule, message)
			}
			// The sentence names the bound it enforces. A refusal quoting a number other than the one the
			// value applies tells a person to shorten a drawing the door would have accepted.
			if !strings.Contains(message, ceiling) {
				t.Errorf("%s refused %s with %q, which names no ceiling of %s", door.name, o.rule, message, ceiling)
			}
			if strings.Contains(message, "at most") {
				t.Errorf("%s refused %s with the aggregate ceilings' sentence %q", door.name, o.rule, message)
			}
		}
	}

	// Six refusals wrote nothing: the board still holds the drawing it was created with at the revision it
	// was created at, and the outbox holds the one create and no update.
	if got, revision := storedDrawingAt(t, router, at); got != drawingA || revision != 1 {
		t.Fatalf("the refusals left the drawing at %s revision %d, want the one it was created with at revision 1", got, revision)
	}
	if n := count(t, admin, s.Event(rest.Created)); n != 1 {
		t.Errorf("%d created events for one accepted create and six refused ones", n)
	}
	if n := count(t, admin, s.Event(rest.Updated)); n != 0 {
		t.Errorf("%d updates for six refused writes", n)
	}
}
