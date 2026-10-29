package rest_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// tracedBoardRow is a row whose drawing and title are written by one body. The drawing
// is nullable and the board starts with nothing on it, so the save moves two fields at
// once while each keeps its own before-image: one held strokes, one held a title.
type tracedBoardRow struct {
	crud.Base
	Title   string          `json:"title"`
	Drawing *entity.Sketch  `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Changes []events.Change `json:"changes,omitempty" gorm:"-" hidden:"true"`
}

func (tracedBoardRow) TableName() string { return "rest_traced_boards" }

// SetChanges is events.Recorder.
func (r *tracedBoardRow) SetChanges(changes []events.Change) { r.Changes = changes }

const tracedBoardDDL = `CREATE TABLE rest_traced_boards (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL, deleted_at timestamptz, title text NOT NULL,
 drawing jsonb);
 ALTER TABLE rest_traced_boards ENABLE ROW LEVEL SECURITY;
 ALTER TABLE rest_traced_boards FORCE ROW LEVEL SECURITY;
 CREATE POLICY traced_boards_tenant ON rest_traced_boards
	USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));`

// TestABoardDrawnAndRetitledNamesEveryFieldItMoved checks the trail of one body that
// moves two fields of a row: the change list holds one entry per named field, in the
// sorted order the door documents, each with that field's own halves — and a field the
// row never held leaves no before half at all. The one-field body is pinned beside this
// file; what would go unsaid here is the second field, because a diff that stopped at
// the first name it was given would still answer 200 and still publish an update.
func TestABoardDrawnAndRetitledNamesEveryFieldItMoved(t *testing.T) {
	s := rest.Spec[*tracedBoardRow]{Module: "trace", Entity: "board", Path: "/boards", Read: "board:read", Write: "board:write"}
	_, router, admin := mountAs(t, s, caller{})
	if _, err := admin.ExecContext(t.Context(), tracedBoardDDL); err != nil {
		t.Fatal(err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/trace/boards", `{"title":"Untitled"}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s, want the board with nothing drawn", code, body)
	}
	at := "/api/v1/trace/boards/" + id(t, body)
	if code, body := call(t, router, http.MethodPatch, at, `{"title":"Traced","drawing":`+drawingA+`}`); code != http.StatusOK {
		t.Fatalf("the save = %d %s, want the board drawn and titled", code, body)
	}

	payload := payloadOf(t, admin, s.Event(rest.Updated))
	var out struct {
		Changes []events.Change `json:"changes"`
	}
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		t.Fatalf("the published payload is not json: %v (%s)", err, payload)
	}
	if len(out.Changes) != 2 {
		t.Fatalf("the payload carries %d changes, want the drawing and the title: %s", len(out.Changes), payload)
	}
	drawn, titled := out.Changes[0], out.Changes[1]
	if drawn.Field != "drawing" || titled.Field != "title" {
		t.Fatalf("the payload names %q then %q, want drawing then title: %s", drawn.Field, titled.Field, payload)
	}
	if len(drawn.Before) != 0 {
		t.Errorf("the change says the board held %s before, want no before half the board never had", drawn.Before)
	}
	if moved := strokes(t, drawn.After); !reflect.DeepEqual(moved, strokes(t, []byte(drawingA))) {
		t.Errorf("the change says the board moved to %s, want the strokes the body wrote", drawn.After)
	}
	if string(titled.Before) != `"Untitled"` || string(titled.After) != `"Traced"` {
		t.Errorf("the change says the title moved %s -> %s, want \"Untitled\" -> \"Traced\"", titled.Before, titled.After)
	}
}

// strokes is a drawing read back as the value type, so a case compares the board a change
// describes and not the way one payload happened to space or normalise it.
func strokes(t *testing.T, raw []byte) entity.Sketch {
	t.Helper()
	var s entity.Sketch
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("%s is not a drawing: %v", raw, err)
	}
	return s
}
