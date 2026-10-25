package screens_test

// The record page's history panel posts the row's own write count beside every
// restore, so a person bringing back an old version writes against the state they
// actually read. It reads that count out of the row the page was drawn from.
// A stepped update form makes the transport hand that row its numbers as
// json.Number rather than the float64 ordinary reads give: the same row, the same
// field, another dynamic type. Then the panel quotes nothing, and a restore over a
// row someone else has since saved writes as plainly as if the entity kept no
// revision at all — while the same record, answering as one flat form, still
// quotes it. Both shapes are pinned here because one page owes what the other
// already pays.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// steppedHistoryServer mounts the note over a real table, with one version in its
// series, and answers either as steps or as the flat form it was before.
func steppedHistoryServer(t *testing.T, steps bool) http.Handler {
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
	if steps {
		fixture.Present.Forms.Update = fixture.Present.Forms.Create
	} else {
		fixture.Present.Forms = nil
	}
	spec := rest.Spec[*steppedRow]{Module: "note", Entity: "note", Path: "/notes", Read: "note:read",
		Write: "note:write", Immutable: []string{"status"}, Present: fixture.Present}
	spec.Mount(api.Surfaces("note"))
	res := api.Resources()[0]
	res.History = &httpx.History{RestoreVerb: "restore", Versions: func(context.Context, uuid.UUID) ([]httpx.Version, error) {
		return []httpx.Version{{ID: uuid.New(), Version: 1, Changed: []string{"title"},
			Fields: json.RawMessage(`{"title":"Earlier"}`), At: time.Unix(1700000000, 0)}}, nil
	}}
	screens.Mount(api.Surfaces("note").App, formShell(), opts, res)
	return router
}

// quotedCondition is the write count the record page hands back for its restores.
func quotedCondition(t *testing.T, h http.Handler, item string) string {
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

func TestAFlatRecordPageQuotesItsRevisionToEveryRestore(t *testing.T) {
	h := steppedHistoryServer(t, false)
	created := submitNote(t, h, "Ordinary save")
	if created.Code != http.StatusSeeOther {
		t.Fatalf("create = %d: %s", created.Code, created.Body)
	}
	if got := quotedCondition(t, h, created.Header().Get("Location")); got != "1" {
		t.Fatalf("the flat record page quotes revision %q, want the row's own 1", got)
	}
}

func TestASteppedRecordPageQuotesItsRevisionToEveryRestore(t *testing.T) {
	h := steppedHistoryServer(t, true)
	item := savedSteppedNote(t, h)
	if got := quotedCondition(t, h, item); got != "1" {
		t.Fatalf("the stepped record page quotes revision %q, want the row's own 1: with no condition its restore writes over a row someone else has saved", got)
	}
}

// savedSteppedNote answers the note through its steps and names the record page
// the final submit leaves behind.
func savedSteppedNote(t *testing.T, h http.Handler) string {
	t.Helper()
	at := "/app/note/notes"
	first := continuation(t, stepRequest(h, at+"/new", nil), http.StatusOK, 0)
	second := continuation(t, postStep(h, at+"/new/steps", first, "next", url.Values{"title": {"Stepped save"}}), http.StatusOK, 1)
	last := continuation(t, postStep(h, at+"/new/steps", second, "next", url.Values{"body": {"draft"}, "rank": {"7"}}), http.StatusOK, 2)
	created := postStep(h, at, last, "submit", nil)
	if created.Code != http.StatusSeeOther {
		t.Fatalf("stepped create = %d: %s", created.Code, created.Body)
	}
	return created.Header().Get("Location")
}
