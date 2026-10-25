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

// erasedBoardRow is a drawing a person may rub out entirely. The column is
// nullable, so a row may hold a board with strokes, a board with nothing drawn
// on it, or no board at all, and the write door has to keep those three apart.
type erasedBoardRow struct {
	crud.Base
	Title    string         `json:"title"`
	Drawing  *entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64          `json:"revision" required:"false" readOnly:"true" gorm:"default:1"`
}

func (erasedBoardRow) TableName() string { return "rest_erased_boards" }

const erasedBoardDDL = `CREATE TABLE rest_erased_boards (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL, deleted_at timestamptz, title text NOT NULL,
 drawing jsonb, revision bigint NOT NULL DEFAULT 1);
 ALTER TABLE rest_erased_boards ENABLE ROW LEVEL SECURITY;
 ALTER TABLE rest_erased_boards FORCE ROW LEVEL SECURITY;
 CREATE POLICY erased_boards_tenant ON rest_erased_boards
	USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));`

// answeredBoard is what one answer of this resource says about a row: the field
// the person drew on, read the way the row on the other side holds it, and the
// title beside it.
type answeredBoard struct {
	Title   string          `json:"title"`
	Drawing json.RawMessage `json:"drawing"`
}

func readBoard(t *testing.T, router http.Handler, method, path, body string) (int, answeredBoard, string) {
	t.Helper()
	code, raw := call(t, router, method, path, body)
	var row answeredBoard
	if code == http.StatusOK || code == http.StatusCreated {
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			t.Fatalf("%s %s answered %s: %v", method, path, raw, err)
		}
	}
	return code, row, raw
}

// TestPatchingADrawingToNullTakesTheStrokesOutOfTheRow checks the clear at the
// door a person uses. A body that says null for the drawing takes the board out
// of the row, and the answer the write returns agrees with the row the next read
// sees rather than repeating the strokes it removed. A title the body never named
// keeps what the row held, and the column stays writable afterwards.
func TestPatchingADrawingToNullTakesTheStrokesOutOfTheRow(t *testing.T) {
	s := rest.Spec[*erasedBoardRow]{Module: "erase", Entity: "board", Path: "/boards", Read: "board:read", Write: "board:write"}
	_, router, admin := mountAs(t, s, hosts{map[string]tenancy.Tenant{host: acme}})
	if _, err := admin.ExecContext(t.Context(), erasedBoardDDL); err != nil {
		t.Fatal(err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/erase/boards", `{"title":"Rubbed out","drawing":`+drawingA+`}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s, want the board drawn", code, body)
	}
	at := "/api/v1/erase/boards/" + id(t, body)

	code, cleared, raw := readBoard(t, router, http.MethodPatch, at, `{"drawing":null}`)
	if code != http.StatusOK {
		t.Fatalf("the clear = %d %s, want the drawing taken out of the row", code, raw)
	}
	if string(cleared.Drawing) != "null" {
		t.Errorf("the clear answered drawing %s, want null: the row holds no board", cleared.Drawing)
	}
	code, reread, raw := readBoard(t, router, http.MethodGet, at, "")
	if code != http.StatusOK {
		t.Fatalf("read the cleared row = %d %s", code, raw)
	}
	if string(reread.Drawing) != "null" {
		t.Errorf("the cleared row reads back drawing %s, want null: the strokes are still in the column", reread.Drawing)
	}
	if reread.Title != "Rubbed out" {
		t.Errorf("title = %q, want the %q this body never named", reread.Title, "Rubbed out")
	}
	// The column is not stuck: a board drawn again answers with its strokes.
	if code, _, raw = readBoard(t, router, http.MethodPatch, at, `{"drawing":`+drawingA+`}`); code != http.StatusOK {
		t.Fatalf("drawing again = %d %s", code, raw)
	}
	code, redrawn, raw := readBoard(t, router, http.MethodGet, at, "")
	if code != http.StatusOK || string(redrawn.Drawing) != drawingA {
		t.Errorf("a board drawn over a cleared one reads back %s (%d %s), want the strokes it was given", redrawn.Drawing, code, raw)
	}
}
