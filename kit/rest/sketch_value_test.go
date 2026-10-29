package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type SketchRow struct {
	crud.Base
	Title    string        `json:"title"`
	Drawing  entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64         `json:"revision" required:"false" readOnly:"true" gorm:"default:1"`
}

func (SketchRow) TableName() string { return "rest_drawings" }

const sketchDDL = `CREATE TABLE rest_drawings (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL, deleted_at timestamptz, title text NOT NULL,
 drawing jsonb, revision bigint NOT NULL DEFAULT 1);
 ALTER TABLE rest_drawings ENABLE ROW LEVEL SECURITY;
 ALTER TABLE rest_drawings FORCE ROW LEVEL SECURITY;
 CREATE POLICY drawings_tenant ON rest_drawings USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));`
const drawingA = `[[[0,0,0],[500,250,17],[1000,1000,9007199254740991]],[[900,100,0]]]`

type sketchReplacement struct {
	Drawing          entity.Sketch `json:"drawing" validate:"required"`
	ExpectedRevision int64         `json:"expectedRevision"`
}

func TestSketchWritesPreserveValuesAndRefuseWithoutEffects(t *testing.T) {
	s := rest.Spec[*SketchRow]{Module: "drawing", Entity: "drawing", Path: "/drawings", Read: "drawing:read", Write: "drawing:write"}
	other := tenancy.Tenant{ID: uuid.New(), Slug: "other"}
	_, router, admin := mountAs(t, s, hosts{map[string]tenancy.Tenant{host: acme, "other.test": other}}, func(api *httpx.API) {
		rest.Command(api.Surfaces(s.Module), s, "replace", "Replace drawing", "", []string{s.Event(rest.Updated)}, func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in sketchReplacement) (*SketchRow, error) {
			row, err := crud.GetForUpdate[*SketchRow](tx, id)
			if err != nil {
				return nil, err
			}
			if row.Revision != in.ExpectedRevision {
				return nil, rest.ErrStaleTag
			}
			return s.UpdateRow(ctx, tx, id, map[string]any{"drawing": in.Drawing})
		}, rest.CommandOptions{})
	})
	if _, err := admin.ExecContext(t.Context(), sketchDDL); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"null", "[]", drawingA} {
		code, body := call(t, router, "POST", "/api/v1/drawing/drawings", `{"title":"Sketch","drawing":`+raw+`}`)
		if code != 201 {
			t.Fatalf("create %s: %d %s", raw, code, body)
		}
		rowID := id(t, body)
		code, body = call(t, router, "GET", "/api/v1/drawing/drawings/"+rowID, "")
		var got struct{ Drawing entity.Sketch }
		if code != 200 || json.Unmarshal([]byte(body), &got) != nil {
			t.Fatal(code, body)
		}
		b, _ := json.Marshal(got.Drawing)
		if string(b) != raw {
			t.Fatalf("roundtrip %s != %s", b, raw)
		}
	}
	code, body := call(t, router, "POST", "/api/v1/drawing/drawings", `{"title":"Kept","drawing":`+drawingA+`}`)
	if code != 201 {
		t.Fatal(code, body)
	}
	rowID := id(t, body)
	path := "/api/v1/drawing/drawings/" + rowID
	for _, raw := range []string{`[[[1.0,2,0]]]`, `[[[1e0,2,0]]]`, `[[[null,2,0]]]`, `[[[1,2]]]`, `[[[1001,2,0]]]`, "[" + strings.Repeat("[[0,0,0]],", 400) + "[[0,0,0]]]", "[[" + strings.Repeat("[0,0,0],", 20000) + "[0,0,0]]]"} {
		for _, door := range []struct{ method, path, prefix string }{{"POST", "/api/v1/drawing/drawings", `{"title":"Bad","drawing":`}, {"PATCH", path, `{"drawing":`}, {"POST", path + "/replace", `{"expectedRevision":1,"drawing":`}} {
			code, body = call(t, router, door.method, door.path, door.prefix+raw+`}`)
			if code != 422 || !strings.Contains(body, "drawing") {
				t.Errorf("refusal %s: %d %s", door.path, code, body)
			}
		}
	}
	if n := count(t, admin, s.Event(rest.Created)); n != 4 {
		t.Errorf("refusals created %d rows", n)
	}
	if n := count(t, admin, s.Event(rest.Updated)); n != 0 {
		t.Errorf("refusals emitted %d updates", n)
	}
	for _, method := range []string{"GET", "PATCH"} {
		req := httptest.NewRequest(method, "http://other.test"+path, strings.NewReader(`{"drawing":[]}`))
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 404 || strings.Contains(w.Body.String(), "Kept") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	assertDrawingContention(t, router, path, func() int { return count(t, admin, s.Event(rest.Updated)) })
	for _, query := range []string{"?sort=drawing", "?filter=drawing:x"} {
		code, body = call(t, router, "GET", "/api/v1/drawing/drawings"+query, "")
		if code != 422 {
			t.Fatal(code, body)
		}
	}
	documentAPI, documentRouter := patchDocumentAPI()
	s.Mount(documentAPI.Surfaces(s.Module))
	doc := httptest.NewRecorder()
	documentRouter.ServeHTTP(doc, httptest.NewRequest("GET", "http://"+host+"/openapi.json", nil))
	if !strings.Contains(doc.Body.String(), `"prefixItems"`) {
		t.Fatal("missing tuple schema")
	}
}

func TestSketchFormValuesUseOneJSONControl(t *testing.T) {
	fields := entity.Fields[*SketchRow]()
	for _, raw := range []string{"null", "[]", drawingA} {
		values, err := rest.UpdateValues([]byte(url.Values{"drawing": {raw}}.Encode()), fields, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := json.Marshal(values["drawing"])
		if string(got) != raw {
			t.Fatal(string(got))
		}
	}
	for _, raw := range [][]string{{""}, {" "}, {"[]", "[]"}, {`[[[1.0,2,0]]]`}} {
		if _, err := rest.Values([]byte(url.Values{"drawing": raw}.Encode()), fields, nil); err == nil {
			t.Fatal(raw)
		}
	}
	values, err := rest.UpdateValues(nil, fields, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := values["drawing"]; ok {
		t.Fatal("omission cleared drawing")
	}
}

func assertDrawingContention(t *testing.T, router http.Handler, path string, events func() int) {
	t.Helper()
	type result struct {
		code int
		body string
	}
	var wg sync.WaitGroup
	results := make(chan result, 2)
	for _, raw := range []string{drawingA, `[[[1,2,0]]]`} {
		wg.Go(func() {
			code, body := call(t, router, "POST", path+"/replace", `{"expectedRevision":1,"drawing":`+raw+`}`)
			results <- result{code, body}
		})
	}
	wg.Wait()
	close(results)
	statuses := map[int]int{}
	var winner struct {
		Drawing  entity.Sketch
		Revision int64
	}
	for response := range results {
		statuses[response.code]++
		if response.code == 200 {
			if err := json.Unmarshal([]byte(response.body), &winner); err != nil {
				t.Fatal(err)
			}
		} else if strings.Contains(response.body, `"drawing":`) {
			t.Fatal("refusal returned a row", response.body)
		}
	}
	if statuses[200] != 1 || statuses[412] != 1 || events() != 1 {
		t.Fatal(statuses)
	}
	code, body := call(t, router, "GET", path, "")
	var saved struct {
		Drawing  entity.Sketch
		Revision int64
	}
	if code != 200 || json.Unmarshal([]byte(body), &saved) != nil {
		t.Fatal(code, body)
	}
	want, _ := json.Marshal(winner.Drawing)
	got, _ := json.Marshal(saved.Drawing)
	if string(want) != string(got) || saved.Revision != 2 {
		t.Fatal("stored value is not the winner", body)
	}
}

func TestSketchDirectRefusalAndOutboxFailureRollBack(t *testing.T) {
	s := rest.Spec[*SketchRow]{Module: "drawing", Entity: "drawing", Path: "/drawings", Read: "drawing:read", Write: "drawing:write"}
	_, router, admin := mountAs(t, s, hosts{map[string]tenancy.Tenant{host: acme}}, func(api *httpx.API) {
		rest.Command(api.Surfaces(s.Module), s, "invalid", "Invalid drawing", "", []string{s.Event(rest.Updated)}, func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in struct {
			Points bool `json:"points"`
		}) (*SketchRow, error) {
			bad := make(entity.Sketch, 401)
			if in.Points {
				bad = entity.Sketch{make(entity.SketchStroke, 20001)}
			}
			row, err := s.CreateRow(ctx, tx, &SketchRow{Title: "Refused", Drawing: bad})
			if row != nil || !errors.Is(err, entity.ErrInvalidSketch) {
				t.Errorf("direct create: %v %v", row, err)
			}
			row, err = s.UpdateRow(ctx, tx, id, map[string]any{"drawing": bad})
			if row != nil || !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("direct update: %v %v", row, err)
			}
			return row, err
		}, rest.CommandOptions{})
	})
	if _, err := admin.ExecContext(t.Context(), sketchDDL); err != nil {
		t.Fatal(err)
	}
	code, body := call(t, router, "POST", "/api/v1/drawing/drawings", `{"title":"Kept","drawing":`+drawingA+`}`)
	if code != 201 {
		t.Fatal(code, body)
	}
	path := "/api/v1/drawing/drawings/" + id(t, body)
	for _, flag := range []string{"true", "false"} {
		code, body = call(t, router, "POST", path+"/invalid", `{"points":`+flag+`}`)
		if code != 422 {
			t.Fatal(code, body)
		}
	}
	if _, err := admin.ExecContext(t.Context(), `ALTER TABLE platformkit_outbox ADD CONSTRAINT reject_drawing_update CHECK (name <> 'drawing.drawing.updated')`); err != nil {
		t.Fatal(err)
	}
	code, body = call(t, router, "PATCH", path, `{"drawing":[]}`)
	if code < 400 {
		t.Fatal(code, body)
	}
	code, body = call(t, router, "GET", path, "")
	var saved struct {
		Drawing  entity.Sketch
		Revision int64
	}
	if code != 200 || json.Unmarshal([]byte(body), &saved) != nil {
		t.Fatal(code, body)
	}
	raw, _ := json.Marshal(saved.Drawing)
	if string(raw) != drawingA || saved.Revision != 1 || count(t, admin, s.Event(rest.Updated)) != 0 || count(t, admin, s.Event(rest.Created)) != 1 {
		t.Fatal(body)
	}
}
