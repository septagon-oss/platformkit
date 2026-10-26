package rest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// redrawnBoardRow is the row a generated edit page posts: a title a person retypes, the
// drawing the page carries with it, and the write count the form was drawn from. The
// drawing is nullable so a page can be drawn over a board nobody has drawn on, which is
// one of the states a refused body has to leave untouched.
type redrawnBoardRow struct {
	crud.Base
	Title    string         `json:"title"`
	Drawing  *entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64          `json:"revision" required:"false" readOnly:"true" gorm:"default:1"`
}

func (redrawnBoardRow) TableName() string { return "rest_redrawn_boards" }

const redrawnBoardDDL = `CREATE TABLE rest_redrawn_boards (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL, deleted_at timestamptz, title text NOT NULL,
 drawing jsonb, revision bigint NOT NULL DEFAULT 1);
 ALTER TABLE rest_redrawn_boards ENABLE ROW LEVEL SECURITY;
 ALTER TABLE rest_redrawn_boards FORCE ROW LEVEL SECURITY;
 CREATE POLICY redrawn_boards_tenant ON rest_redrawn_boards
	USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));`

// redrawnBoard is one row read back through the door that wrote it, so the case judges the
// row and its write count rather than the answer the refused request happened to print.
type redrawnBoard struct {
	Title    string          `json:"title"`
	Drawing  json.RawMessage `json:"drawing"`
	Revision int64           `json:"revision"`
}

func readRedrawnBoard(t *testing.T, router http.Handler, at string) redrawnBoard {
	t.Helper()
	code, raw := call(t, router, http.MethodGet, at, "")
	if code != http.StatusOK {
		t.Fatalf("read the row = %d %s", code, raw)
	}
	var row redrawnBoard
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatalf("the row is not json: %v (%s)", err, raw)
	}
	return row
}

// TestAFormBodyThatRetitlesAndOverdrawsMovesNeitherField checks the same refusal as one
// whole body at the door a posted form arrives at: a form that retypes the board's title
// and carries a drawing past the declared stroke ceiling is refused, and none of the row
// moves — the title keeps what the row held, the board keeps its strokes, its own write
// count does not tick, and no update is published.
//
// The form door is a second door and not a second spelling of the first: it reads the body
// field by field into a map of values and hands that map to the write, so a half-read form
// is one value away from a half-written row. A door that dropped the field it could not
// read and kept the ones it could would answer this request with the same refusal while
// leaving the new title in the row.
func TestAFormBodyThatRetitlesAndOverdrawsMovesNeitherField(t *testing.T) {
	s := rest.Spec[*redrawnBoardRow]{Module: "redrawn", Entity: "board", Path: "/boards", Read: "board:read", Write: "board:write"}
	_, router, admin := mountAs(t, s, hosts{map[string]tenancy.Tenant{host: acme}}, func(api *httpx.API) {
		// One command stands where the generated page's write stands: parse the form the
		// page posted, then write through the orchestration every door shares.
		rest.Command(api.Surfaces(s.Module), s, "form", "Write from a posted form", "", []string{s.Event(rest.Updated)}, func(ctx context.Context, tx db.Tx[db.Tenant], rowID uuid.UUID, in struct {
			Body string `json:"body"`
		}) (*redrawnBoardRow, error) {
			sent, err := rest.UpdateValues([]byte(in.Body), crud.Fields[*redrawnBoardRow](), nil)
			if err != nil {
				return nil, err
			}
			return s.UpdateRow(ctx, tx, rowID, sent)
		}, rest.CommandOptions{})
	})
	if _, err := admin.ExecContext(t.Context(), redrawnBoardDDL); err != nil {
		t.Fatal(err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/redrawn/boards", `{"title":"Held","drawing":`+drawingA+`}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s, want the board titled and drawn", code, body)
	}
	at := "/api/v1/redrawn/boards/" + id(t, body)
	held := readRedrawnBoard(t, router, at)
	if held.Title != "Held" || string(held.Drawing) != drawingA || held.Revision != 1 {
		t.Fatalf("the board the case writes down is %s", body)
	}

	over := square(entity.SketchMaxStrokes+1, entity.SketchMaxStrokes+1)
	form := mustJSON(t, map[string]string{"body": url.Values{"title": {"Renamed"}, "drawing": {over}}.Encode()})
	if code, answer := call(t, router, http.MethodPost, at+"/form", form); code < http.StatusBadRequest || code >= http.StatusInternalServerError {
		t.Fatalf("the two-field form = %d %s, want the ceiling refused", code, answer)
	}

	refused := readRedrawnBoard(t, router, at)
	if refused.Title != "Held" {
		t.Errorf("the refused form left title %q, want the %q the row held", refused.Title, "Held")
	}
	if string(refused.Drawing) != drawingA {
		t.Errorf("the refused form left drawing %s, want the strokes the row held", refused.Drawing)
	}
	if refused.Revision != held.Revision {
		t.Errorf("the refused form moved the write count to %d, want the %d it was refused at", refused.Revision, held.Revision)
	}
	if n := count(t, admin, s.Event(rest.Updated)); n != 0 {
		t.Errorf("%d updates published for one refused form", n)
	}
	// The row is not stuck: the title that refused form asked for is written once the
	// drawing it carried is taken out of it.
	if code, answer := call(t, router, http.MethodPost, at+"/form", mustJSON(t, map[string]string{"body": url.Values{"title": {"Renamed"}}.Encode()})); code != http.StatusOK {
		t.Fatalf("the retitle alone = %d %s, want the row writable again", code, answer)
	}
	if renamed := readRedrawnBoard(t, router, at); renamed.Title != "Renamed" || string(renamed.Drawing) != drawingA || renamed.Revision != held.Revision+1 {
		t.Errorf("the retitle alone left %s, want the new title over the strokes that form never named", mustJSON(t, renamed))
	}
}
