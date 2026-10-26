package rest_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// keptBoardRow is a board whose title a person may rename while a drawing is refused:
// one body holding one field inside the ceiling and one field past it. The drawing is
// nullable so a row can be read with nothing on it, which is one of the states the
// refusal has to leave untouched.
type keptBoardRow struct {
	crud.Base
	Title   string         `json:"title"`
	Drawing *entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
}

func (keptBoardRow) TableName() string { return "rest_kept_boards" }

const keptBoardDDL = `CREATE TABLE rest_kept_boards (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL, deleted_at timestamptz, title text NOT NULL,
 drawing jsonb);
 ALTER TABLE rest_kept_boards ENABLE ROW LEVEL SECURITY;
 ALTER TABLE rest_kept_boards FORCE ROW LEVEL SECURITY;
 CREATE POLICY kept_boards_tenant ON rest_kept_boards
	USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));`

// keptBoard is one row read back through the door that wrote it, so the case judges the
// row rather than the answer the refused request happened to print.
type keptBoard struct {
	Title   string          `json:"title"`
	Drawing json.RawMessage `json:"drawing"`
}

func readKeptBoard(t *testing.T, router http.Handler, at string) keptBoard {
	t.Helper()
	code, raw := call(t, router, http.MethodGet, at, "")
	if code != http.StatusOK {
		t.Fatalf("read the row = %d %s", code, raw)
	}
	var row keptBoard
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatalf("the row is not json: %v (%s)", err, raw)
	}
	return row
}

// TestARetitledBoardRefusesADrawingOverTheCeilingAndMovesNeitherField checks the refusal
// of a two-field body: one patch that renames the board and draws past the declared
// ceiling is refused, and neither half of the row moves — the title keeps what the row
// held before the body arrived, the board keeps the strokes it was drawn with, and no
// update is published. A door that validated a body field by field would answer this
// request with the same status and the same reason while leaving the new title in the row.
func TestARetitledBoardRefusesADrawingOverTheCeilingAndMovesNeitherField(t *testing.T) {
	s := rest.Spec[*keptBoardRow]{Module: "kept", Entity: "board", Path: "/boards", Read: "board:read", Write: "board:write"}
	_, router, admin := mountAs(t, s, hosts{map[string]tenancy.Tenant{host: acme}})
	if _, err := admin.ExecContext(t.Context(), keptBoardDDL); err != nil {
		t.Fatal(err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/kept/boards", `{"title":"Held","drawing":`+drawingA+`}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s, want the board titled and drawn", code, body)
	}
	at := "/api/v1/kept/boards/" + id(t, body)

	over := square(entity.SketchMaxStrokes+1, entity.SketchMaxStrokes+1)
	if code, answer := call(t, router, http.MethodPatch, at, `{"title":"Renamed","drawing":`+over+`}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("the two-field body = %d %s, want the ceiling refused", code, answer)
	}

	held := readKeptBoard(t, router, at)
	if held.Title != "Held" {
		t.Errorf("the refused body left title %q, want the %q the row held", held.Title, "Held")
	}
	if string(held.Drawing) != drawingA {
		t.Errorf("the refused body left drawing %s, want the strokes the row held", held.Drawing)
	}
	if n := count(t, admin, s.Event(rest.Updated)); n != 0 {
		t.Errorf("%d updates published for one refused body", n)
	}
	// The row is not stuck: the title the refused body asked for is written once the
	// drawing it carried is taken out of the request.
	if code, answer := call(t, router, http.MethodPatch, at, `{"title":"Renamed"}`); code != http.StatusOK {
		t.Fatalf("the retitle alone = %d %s, want the row writable again", code, answer)
	}
	if renamed := readKeptBoard(t, router, at); renamed.Title != "Renamed" || string(renamed.Drawing) != drawingA {
		t.Errorf("the retitle left title %q drawing %s, want the new title over the strokes it never named", renamed.Title, renamed.Drawing)
	}
}
