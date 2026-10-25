package rest_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// repaintedBoardRow is a row a person draws over: the drawing is nullable and
// writable more than once. It opts into events.Recorder with the one field and
// the one method, which is what puts the door's own before-image into the payload
// the outbox holds.
type repaintedBoardRow struct {
	crud.Base
	Title    string          `json:"title"`
	Drawing  *entity.Sketch  `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64           `json:"revision" required:"false" readOnly:"true" gorm:"default:1"`
	Changes  []events.Change `json:"changes,omitempty" gorm:"-" hidden:"true"`
}

func (repaintedBoardRow) TableName() string { return "rest_repainted_boards" }

// SetChanges is events.Recorder.
func (r *repaintedBoardRow) SetChanges(changes []events.Change) { r.Changes = changes }

const repaintedBoardDDL = `CREATE TABLE rest_repainted_boards (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL, deleted_at timestamptz, title text NOT NULL,
 drawing jsonb, revision bigint NOT NULL DEFAULT 1);
 ALTER TABLE rest_repainted_boards ENABLE ROW LEVEL SECURITY;
 ALTER TABLE rest_repainted_boards FORCE ROW LEVEL SECURITY;
 CREATE POLICY repainted_boards_tenant ON rest_repainted_boards
	USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));`

// publishedChanges is the changes member of the last <entity>.updated payload the
// outbox holds, in the shape the trail reads it.
type publishedChanges struct {
	Changes []struct {
		Field  string          `json:"field"`
		Before json.RawMessage `json:"before"`
		After  json.RawMessage `json:"after"`
	} `json:"changes"`
}

// TestAClearedDrawingSaysWhichStrokesItTookAway checks that the event tells the
// truth the row now tells: a body that rubs the board out publishes an update whose
// change names the drawing and carries the strokes it removed as its before half.
// The answer and the row are pinned elsewhere at this door; this is the half a
// subscriber — the audit trail, an index, a phone that caches the board — reads.
func TestAClearedDrawingSaysWhichStrokesItTookAway(t *testing.T) {
	s := rest.Spec[*repaintedBoardRow]{Module: "repaint", Entity: "board", Path: "/boards", Read: "board:read", Write: "board:write"}
	_, router, admin := mountAs(t, s, hosts{map[string]tenancy.Tenant{host: acme}})
	if _, err := admin.ExecContext(t.Context(), repaintedBoardDDL); err != nil {
		t.Fatal(err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/repaint/boards", `{"title":"First pass","drawing":`+drawingA+`}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s, want the board drawn", code, body)
	}
	at := "/api/v1/repaint/boards/" + id(t, body)
	if code, body := call(t, router, http.MethodPatch, at, `{"drawing":null}`); code != http.StatusOK {
		t.Fatalf("the clear = %d %s", code, body)
	}

	payload := payloadOf(t, admin, s.Event(rest.Updated))
	var out publishedChanges
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		t.Fatalf("the published payload is not json: %v (%s)", err, payload)
	}
	if len(out.Changes) != 1 {
		t.Fatalf("the payload carries %d changes, want the drawing alone: %s", len(out.Changes), payload)
	}
	c := out.Changes[0]
	if c.Field != "drawing" {
		t.Errorf("the change names %q, want the drawing the body named", c.Field)
	}
	if strokesOf(t, c.Before) != strokesOf(t, []byte(drawingA)) {
		t.Errorf("the change says the board held %s, want the strokes the clear took away", c.Before)
	}
	if string(c.After) != "null" {
		t.Errorf("the change says the board moved to %s, want null", c.After)
	}
}

// strokesOf is a drawing read back and written again, so a case compares the board
// a change describes and not the way one payload happened to space it.
func strokesOf(t *testing.T, raw []byte) string {
	t.Helper()
	var s entity.Sketch
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("%s is not a drawing: %v", raw, err)
	}
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("%s is not a drawing: %v", raw, err)
	}
	return string(out)
}
