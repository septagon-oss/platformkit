package rest_test

import (
	"context"
	"encoding/json"
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

// drawingB is a second legal drawing, distinguishable from drawingA point by point.
const drawingB = `[[[10,20,0],[30,40,5]],[[500,500,0]]]`

// TestAFormWriteKeepsTheDrawingAndRefusesLimitsWithoutEffects drives the door the
// generated screens stand at — rest.UpdateValues over a posted form, then this
// Spec's own write inside the request transaction — because a value that only
// survives a JSON body has not survived the control a person submits.
func TestAFormWriteKeepsTheDrawingAndRefusesLimitsWithoutEffects(t *testing.T) {
	s := rest.Spec[*SketchRow]{Module: "drawing", Entity: "drawing", Path: "/drawings", Read: "drawing:read", Write: "drawing:write"}
	_, router, admin := mountAs(t, s, hosts{map[string]tenancy.Tenant{host: acme}}, func(api *httpx.API) {
		// One command stands where the generated page's write stands: parse the form
		// the page posted, then write through the orchestration every door shares.
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
	code, body := call(t, router, "POST", "/api/v1/drawing/drawings", `{"title":"Form","drawing":`+drawingA+`}`)
	if code != 201 {
		t.Fatal(code, body)
	}
	path := "/api/v1/drawing/drawings/" + id(t, body)
	stored := func() (string, int64) {
		t.Helper()
		code, body := call(t, router, "GET", path, "")
		if code != 200 {
			t.Fatal(code, body)
		}
		var got struct {
			Drawing  json.RawMessage `json:"drawing"`
			Revision int64           `json:"revision"`
		}
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatal(err)
		}
		return string(got.Drawing), got.Revision
	}
	// The two doors answer one value the same way: the map a posted form parses to
	// re-marshals to the JSON the same body posted as JSON stores.
	formValues, err := rest.UpdateValues([]byte(url.Values{"drawing": {drawingB}}.Encode()), crud.Fields[*SketchRow](), nil)
	if err != nil {
		t.Fatal(err)
	}
	var jsonValues map[string]json.RawMessage
	if err := json.Unmarshal([]byte(`{"drawing":`+drawingB+`}`), &jsonValues); err != nil {
		t.Fatal(err)
	}
	fromForm, err := json.Marshal(formValues["drawing"])
	if err != nil {
		t.Fatal(err)
	}
	if string(fromForm) != string(jsonValues["drawing"]) {
		t.Fatalf("the form door reads %s where the json door reads %s", fromForm, jsonValues["drawing"])
	}

	// A legal form write keeps every integer and moves the row's own count once.
	before, revision := stored()
	code, body = call(t, router, "POST", path+"/form", mustJSON(t, map[string]string{"body": url.Values{"drawing": {drawingB}}.Encode()}))
	if code != 200 {
		t.Fatal(code, body)
	}
	if got, rev := stored(); got != drawingB || rev != revision+1 {
		t.Fatalf("form write stored %s at revision %d, want %s at %d", got, rev, drawingB, revision+1)
	}
	before, revision = stored()

	// Beyond either limit the door refuses, and the refusal is the whole of what
	// happens: no column written, no event published, no row in the answer.
	for _, drawing := range []string{
		"[" + strings.Repeat("[[0,0,0]],", 400) + "[[0,0,0]]]",
		"[[" + strings.Repeat("[0,0,0],", 20000) + "[0,0,0]]]",
		"[[[0,0,0],[1001,0,1]]]",
		"[[[0,0,1]]]",
	} {
		code, answer := call(t, router, "POST", path+"/form", mustJSON(t, map[string]string{"body": url.Values{"drawing": {drawing}}.Encode()}))
		if code < 400 || code >= 500 || !strings.Contains(answer, "drawing") || strings.Contains(answer, `"revision":`) {
			t.Errorf("form refusal for %s…: %d %s", drawing[:24], code, answer)
		}
		if got, rev := stored(); got != before || rev != revision {
			t.Fatalf("a refused form write moved the row to %s at revision %d", got, rev)
		}
	}
	if n := count(t, admin, s.Event(rest.Updated)); n != 1 {
		t.Errorf("%d updates for one accepted form write and four refused ones", n)
	}

	// A drawing a person cleared posts "[]" and is stored as explicitly empty, which
	// is a different answer from the field being absent from the form at all.
	code, body = call(t, router, "POST", path+"/form", mustJSON(t, map[string]string{"body": url.Values{"drawing": {"[]"}}.Encode()}))
	if code != 200 {
		t.Fatal(code, body)
	}
	if got, _ := stored(); got != "[]" {
		t.Fatalf("a cleared drawing stored %s", got)
	}
	_, revision = stored()
	code, body = call(t, router, "POST", path+"/form", mustJSON(t, map[string]string{"body": url.Values{"title": {"Untouched"}}.Encode()}))
	if code != 200 {
		t.Fatal(code, body)
	}
	if got, rev := stored(); got != "[]" || rev != revision+1 {
		t.Fatalf("a form that named no drawing stored %s at revision %d", got, rev)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
