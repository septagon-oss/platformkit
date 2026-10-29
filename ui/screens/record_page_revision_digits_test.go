package screens_test

// The count beside a restore is a promise about the row, not about the page.
//
// A record page hands its history panel the row's own write count so every restore
// it offers writes against the state the person standing there actually read. A
// stepped update form makes the transport decode that row with exact numbers, so
// the count reaches the panel as a json.Number rather than the float64 an ordinary
// read answers with. Two things hold the panel to the row through that: the count it
// quotes is the one the row holds after every write, not the one the create left
// behind, and it keeps the digits of a count a float64 cannot hold — which is the
// reason the exact read exists at all.
//
// The panel offers a restore only where the Spec mounts the command it names, so the
// fixture declares History the way a product does: kit/rest keeps the series and
// mounts the door beside it, and the page has somewhere to post what it quotes.

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// countedNoteServer mounts the versioned stepped note and hands back the database
// handle, so a case can ask the row what it counts itself as.
func countedNoteServer(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	admin, conn := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), `CREATE TABLE notes (
	 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, created_at timestamptz NOT NULL,
	 updated_at timestamptz NOT NULL, deleted_at timestamptz, title text NOT NULL,
	 body text NOT NULL DEFAULT '', status text NOT NULL DEFAULT 'open', rank bigint NOT NULL DEFAULT 0,
	 pinned boolean NOT NULL DEFAULT false, revision bigint NOT NULL DEFAULT 1);
	 ALTER TABLE notes ENABLE ROW LEVEL SECURITY; ALTER TABLE notes FORCE ROW LEVEL SECURITY;
	 CREATE POLICY notes_tenant ON notes USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));`); err != nil {
		t.Fatal(err)
	}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: formHost, Tenants: formDirectory{}, Authorize: formDirectory{},
		Conn: conn, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.MustParse("22222222-2222-4222-8222-222222222222")}, true, nil
		},
	})
	fixture := steppedNote()
	fixture.Present.Forms.Update = fixture.Present.Forms.Create
	spec := rest.Spec[*steppedRow]{Module: "note", Entity: "note", Path: "/notes", Read: "note:read",
		Write: "note:write", Immutable: []string{"status"}, Present: fixture.Present,
		History: true, Versions: &writtenVersions{}}
	spec.Mount(api.Surfaces("note"))
	screens.Mount(api.Surfaces("note").App, formShell(), opts, api.Resources()[0])
	return router, admin
}

// offeredRestore is the count the record page puts beside the restore it offers.
func offeredRestore(t *testing.T, h http.Handler, item string) string {
	t.Helper()
	out := stepRequest(h, item, nil)
	if out.Code != http.StatusOK {
		t.Fatalf("the record page = %d, want its history panel: %s", out.Code, out.Body)
	}
	match := regexp.MustCompile(`name="expectedRevision" value="([^"]*)"`).FindStringSubmatch(out.Body.String())
	if len(match) != 2 {
		t.Fatalf("the record page offers a restore that carries no condition at all: %s", out.Body)
	}
	return strings.TrimSpace(match[1])
}

// savedRowRevision asks the row itself how many writes it counts.
func savedRowRevision(t *testing.T, admin *sql.DB) int {
	t.Helper()
	var revision int
	if err := admin.QueryRowContext(t.Context(), `SELECT revision FROM notes`).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	return revision
}

func TestASteppedRecordPageQuotesTheRevisionItsRowNowHolds(t *testing.T) {
	h, admin := countedNoteServer(t)
	item := savedSteppedNote(t, h)
	for writes := 1; writes <= 3; writes++ {
		quoted := offeredRestore(t, h, item)
		stored := savedRowRevision(t, admin)
		if quoted != strconv.Itoa(writes) || stored != writes {
			t.Fatalf("the record page offers its restores against revision %q while the row holds %d, want both %d: a restore that quotes an older count writes over what was saved since", quoted, stored, writes)
		}
		state := readyNote(t, h, item+"/edit", "Write "+strconv.Itoa(writes+1))
		if out := postStep(h, item, state, "submit", nil); out.Code != http.StatusSeeOther {
			t.Fatalf("save %d: %d %s", writes, out.Code, out.Body)
		}
	}
}

func TestASteppedRecordPageKeepsTheDigitsOfALargeRevision(t *testing.T) {
	h, admin := countedNoteServer(t)
	item := savedSteppedNote(t, h)
	result, err := admin.ExecContext(t.Context(), `UPDATE notes SET revision = 9007199254740993`)
	if err != nil {
		t.Fatal(err)
	}
	if moved, err := result.RowsAffected(); err != nil || moved != 1 {
		t.Fatalf("the fixture's own row moved %d times: %v", moved, err)
	}
	if quoted := offeredRestore(t, h, item); quoted != "9007199254740993" {
		t.Fatalf("the record page offers its restore against revision %q, want the digits the row holds: %d", quoted, savedRowRevision(t, admin))
	}
}
