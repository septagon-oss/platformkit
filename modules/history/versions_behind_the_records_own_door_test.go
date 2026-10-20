package history_test

// This file puts the real version store behind the real record door: a rest.Spec
// mounted with History over the table modules/history owns, driven over HTTP by two
// tenants. kit/rest's own cases record into a slice and the store's own cases never
// open a route, so the claims that need both halves — the policy on the table under
// the kit's transaction, the UNIQUE constraint under two concurrent saves, a restore
// and its refusal leaving the stored rows byte-identical — are asked here.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/history"
)

const (
	acmeHost   = "acme.test"
	globexHost = "globex.test"
	memoRead   = "memo:read"
	memoWrite  = "memo:write"
)

var (
	acme      = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	globex    = tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	principal = uuid.New()
)

// Memo is a record whose writes are kept: Revision is the write count the versions
// are numbered with, and Status is immutable through the generated door.
type Memo struct {
	crud.Base
	Title    string `json:"title" validate:"required"`
	Body     string `json:"body,omitempty" gorm:"type:text"`
	Status   string `json:"status,omitempty"`
	Revision int64  `json:"revision" gorm:"not null;default:1" readOnly:"true" required:"false" doc:"This row's own write count, from 1"`
}

func (Memo) TableName() string { return "history_memos" }

func (m *Memo) Validate(context.Context) error {
	m.Title = strings.TrimSpace(m.Title)
	if m.Title == "" {
		return errors.New("a memo needs a title")
	}
	return nil
}

const memoDDL = `
CREATE TABLE history_memos (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,
	title text NOT NULL,
	body text NOT NULL DEFAULT '',
	status text NOT NULL DEFAULT 'draft',
	revision bigint NOT NULL DEFAULT 1
);
ALTER TABLE history_memos ENABLE ROW LEVEL SECURITY;
ALTER TABLE history_memos FORCE ROW LEVEL SECURITY;
CREATE POLICY history_memos_tenant ON history_memos
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`

// hosts resolves the two tenants from the two hosts the cases call.
type hosts struct{ byHost map[string]tenancy.Tenant }

func (h hosts) ByHost(_ context.Context, _ db.Tx[db.System], host string) (tenancy.Tenant, error) {
	tenant, ok := h.byHost[host]
	if !ok {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return tenant, nil
}

// grants is an authorizer that holds exactly the named permissions.
type grants map[string]bool

func (g grants) Allowed(_ context.Context, _ tenancy.Tenant, grant tenancy.Grant) (bool, error) {
	return g[grant.Permission], nil
}

// world is one schema with the version table, the memo table and the module's store;
// mount puts the record door over it for one authorizer, so two doors with different
// grants can share the rows.
type world struct {
	t     *testing.T
	admin *sql.DB
	app   *db.Conn
	store httpx.Versions
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, app := dbtest.Schema(t, history.Migrations)
	if _, err := admin.ExecContext(t.Context(), memoDDL); err != nil {
		t.Fatalf("create the memo table: %v", err)
	}
	_, store, _, err := history.New(history.Deps{})
	if err != nil {
		t.Fatalf("build the history module: %v", err)
	}
	return &world{t: t, admin: admin, app: app, store: store}
}

func (w *world) mount(authorize httpx.Authorizer) http.Handler {
	w.t.Helper()
	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: acmeHost, Installation: acmeHost,
		Tenants: hosts{byHost: map[string]tenancy.Tenant{acmeHost: acme, globexHost: globex}},
		Conn:    w.app, Authorize: authorize,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	s := rest.Spec[*Memo]{
		Module: "memos", Entity: "memo", Path: "/memo", Name: rest.Name{One: "Memo", Other: "Memos"},
		Read: memoRead, Write: memoWrite,
		History: true, Versions: w.store, Immutable: []string{"status"},
	}
	s.Mount(api.Surfaces(s.Module))
	if err := api.ValidateDeclarations(); err != nil {
		w.t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router
}

func call(t *testing.T, r http.Handler, host, method, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func address(id uuid.UUID) string { return "/api/v1/memos/memo/" + id.String() }

func create(t *testing.T, r http.Handler, host, title string) uuid.UUID {
	t.Helper()
	code, body := call(t, r, host, http.MethodPost, "/api/v1/memos/memo", `{"title":"`+title+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST the memo = %d %s", code, body)
	}
	var out struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.ID == uuid.Nil {
		t.Fatalf("no id in %s: %v", body, err)
	}
	return out.ID
}

func edit(t *testing.T, r http.Handler, host string, id uuid.UUID, title string) (int, string) {
	t.Helper()
	return call(t, r, host, http.MethodPatch, address(id), `{"title":"`+title+`"}`)
}

func restore(t *testing.T, r http.Handler, host string, id uuid.UUID, version, expected int64) (int, string) {
	t.Helper()
	return call(t, r, host, http.MethodPost, address(id)+"/restore",
		`{"version":`+strconv.FormatInt(version, 10)+`,"expectedRevision":`+strconv.FormatInt(expected, 10)+`}`)
}

// storedVersion is one row of the version table as the owner connection reads it,
// with the document and the instant as text so two readings compare byte for byte.
type storedVersion struct {
	Version int64
	Tenant  uuid.UUID
	Fields  string
	Reason  string
	Actor   sql.NullString
	At      string
}

func (w *world) versionsOf(record uuid.UUID) []storedVersion {
	w.t.Helper()
	rows, err := w.admin.QueryContext(w.t.Context(),
		`SELECT version, tenant_id, fields::text, reason, actor::text, committed_at::text
		   FROM history_versions WHERE record_id = $1 ORDER BY version`, record)
	if err != nil {
		w.t.Fatalf("read the versions: %v", err)
	}
	defer rows.Close()
	var out []storedVersion
	for rows.Next() {
		var v storedVersion
		if err := rows.Scan(&v.Version, &v.Tenant, &v.Fields, &v.Reason, &v.Actor, &v.At); err != nil {
			w.t.Fatalf("scan a version: %v", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		w.t.Fatalf("read the versions: %v", err)
	}
	return out
}

func (w *world) outbox(name string) int {
	w.t.Helper()
	var n int
	if err := w.admin.QueryRowContext(w.t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE name = $1`, name).Scan(&n); err != nil {
		w.t.Fatalf("count %s: %v", name, err)
	}
	return n
}

func (w *world) row(id uuid.UUID) (title string, revision int64) {
	w.t.Helper()
	if err := w.admin.QueryRowContext(w.t.Context(),
		`SELECT title, revision FROM history_memos WHERE id = $1`, id).Scan(&title, &revision); err != nil {
		w.t.Fatalf("read the memo: %v", err)
	}
	return title, revision
}

func titleOf(t *testing.T, document string) string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(document), &doc); err != nil {
		t.Fatalf("the version holds %s, which is no document: %v", document, err)
	}
	title, _ := doc["title"].(string)
	return title
}

// TestAnotherTenantNeitherReadsNorRestoresAVersionThroughTheRecordsAddress is the
// brief's tenant isolation by API, with the store that owns the table behind the
// door: a second tenant holding the first's record id is answered 404 by the series
// read and by the restore, and the table, the outbox and the first tenant's series
// are exactly as the first tenant left them.
func TestAnotherTenantNeitherReadsNorRestoresAVersionThroughTheRecordsAddress(t *testing.T) {
	w := newWorld(t)
	router := w.mount(grants{memoRead: true, memoWrite: true})
	memo := create(t, router, acmeHost, "Acme's draft")
	if code, body := edit(t, router, acmeHost, memo, "Acme's second draft"); code != http.StatusOK {
		t.Fatalf("PATCH as Acme = %d %s", code, body)
	}

	if code, body := call(t, router, globexHost, http.MethodGet, address(memo)+"/versions", ""); code != http.StatusNotFound {
		t.Errorf("Globex reading Acme's versions = %d, want 404: %s", code, body)
	}
	if code, body := restore(t, router, globexHost, memo, 1, 2); code != http.StatusNotFound {
		t.Errorf("Globex restoring Acme's version = %d, want 404: %s", code, body)
	}

	stored := w.versionsOf(memo)
	if len(stored) != 2 {
		t.Fatalf("Acme's record holds %d versions after Globex's attempts, want the two Acme wrote", len(stored))
	}
	for _, v := range stored {
		if v.Tenant != acme.ID {
			t.Errorf("version %d belongs to tenant %s, want Acme's", v.Version, v.Tenant)
		}
	}
	if n := w.outbox(rest.RestoredEvent); n != 0 {
		t.Errorf("Globex's refused restore published %d %s rows, want none", n, rest.RestoredEvent)
	}
	if title, revision := w.row(memo); title != "Acme's second draft" || revision != 2 {
		t.Errorf("Acme's row reads %q on revision %d after Globex's attempts, want it untouched on 2", title, revision)
	}
}

// TestTwoConcurrentSavesOfOneRecordKeepTwoVersionsNumberedOneApart is the UNIQUE
// constraint under the kit's row lock: two saves that arrive together serialise on
// the row, so the versions they keep are n and n+1, never the same number twice and
// never a save lost to the other's version.
func TestTwoConcurrentSavesOfOneRecordKeepTwoVersionsNumberedOneApart(t *testing.T) {
	w := newWorld(t)
	router := w.mount(grants{memoRead: true, memoWrite: true})
	memo := create(t, router, acmeHost, "First draft")

	codes := make([]int, 2)
	bodies := make([]string, 2)
	var wg sync.WaitGroup
	for i, title := range []string{"Left edit", "Right edit"} {
		wg.Go(func() {
			codes[i], bodies[i] = edit(t, router, acmeHost, memo, title)
		})
	}
	wg.Wait()
	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("concurrent PATCH %d = %d %s, want 200", i, code, bodies[i])
		}
	}

	stored := w.versionsOf(memo)
	if len(stored) != 3 {
		t.Fatalf("a create and two saves left %d versions, want 3", len(stored))
	}
	for i, v := range stored {
		if v.Version != int64(i+1) {
			t.Errorf("version %d is numbered %d, want the row's own write count", i+1, v.Version)
		}
	}
	if _, revision := w.row(memo); revision != 3 {
		t.Errorf("the row is on revision %d after two saves, want 3", revision)
	}
	if n := w.outbox("memos.memo.updated"); n != 2 {
		t.Errorf("two saves published %d updated rows, want 2", n)
	}
}

// TestARestoreLeavesEveryOlderVersionAsItWasAndAStaleQuoteWritesNothing is the
// brief's acceptance at the real table: three edits, a restore of the first, and the
// fourth row holds the first's content while rows one to three are byte-identical to
// what they were. Then the same restore quoting the revision the row has left behind
// is refused with 412 and leaves table, outbox and row exactly as the restore left them.
func TestARestoreLeavesEveryOlderVersionAsItWasAndAStaleQuoteWritesNothing(t *testing.T) {
	w := newWorld(t)
	router := w.mount(grants{memoRead: true, memoWrite: true})
	memo := create(t, router, acmeHost, "First draft")
	edit(t, router, acmeHost, memo, "Second draft")
	edit(t, router, acmeHost, memo, "Third draft")
	before := w.versionsOf(memo)
	if len(before) != 3 {
		t.Fatalf("three writes left %d versions, want 3", len(before))
	}

	if code, body := restore(t, router, acmeHost, memo, 1, 3); code != http.StatusOK {
		t.Fatalf("POST restore of version 1 = %d %s", code, body)
	}
	after := w.versionsOf(memo)
	if len(after) != 4 {
		t.Fatalf("the restore left %d versions, want 4", len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("version %d changed under the restore:\n before %+v\n after  %+v", i+1, before[i], after[i])
		}
	}
	restored := after[3]
	if titleOf(t, restored.Fields) != titleOf(t, before[0].Fields) {
		t.Errorf("version 4 holds the title %q, want version 1's %q", titleOf(t, restored.Fields), titleOf(t, before[0].Fields))
	}
	if restored.Reason != "restore:1" {
		t.Errorf("version 4 records the reason %q, want restore:1", restored.Reason)
	}
	if !restored.Actor.Valid || restored.Actor.String != principal.String() {
		t.Errorf("version 4 names %v as its author, want the signed-in %s", restored.Actor, principal)
	}
	if title, revision := w.row(memo); title != "First draft" || revision != 4 {
		t.Errorf("the row reads %q on revision %d, want the first title on revision 4", title, revision)
	}
	if n := w.outbox(rest.RestoredEvent); n != 1 {
		t.Errorf("the restore published %d %s rows, want one", n, rest.RestoredEvent)
	}

	code, body := restore(t, router, acmeHost, memo, 2, 3)
	if code != http.StatusPreconditionFailed {
		t.Fatalf("POST restore quoting revision 3 against a row on 4 = %d %s, want 412", code, body)
	}
	if again := w.versionsOf(memo); len(again) != 4 {
		t.Errorf("the refused restore left %d versions, want the four that stood", len(again))
	} else {
		for i := range after {
			if after[i] != again[i] {
				t.Errorf("version %d changed under a refused restore:\n before %+v\n after  %+v", i+1, after[i], again[i])
			}
		}
	}
	if n := w.outbox(rest.RestoredEvent); n != 1 {
		t.Errorf("the refused restore left %d %s rows, want the one the accepted restore made", n, rest.RestoredEvent)
	}
	if title, revision := w.row(memo); title != "First draft" || revision != 4 {
		t.Errorf("the refused restore left the row at %q on revision %d, want it untouched", title, revision)
	}
}

// TestARestoreIsRefusedToACallerWithoutTheRecordsWritePermission is the guard on the
// generic restore door: it is the record's own write permission, and a caller who may
// read the series may not write a version back. The refusal writes nothing.
func TestARestoreIsRefusedToACallerWithoutTheRecordsWritePermission(t *testing.T) {
	w := newWorld(t)
	editor := w.mount(grants{memoRead: true, memoWrite: true})
	reader := w.mount(grants{memoRead: true})
	memo := create(t, editor, acmeHost, "First draft")
	edit(t, editor, acmeHost, memo, "Second draft")

	if code, body := call(t, reader, acmeHost, http.MethodGet, address(memo)+"/versions", ""); code != http.StatusOK {
		t.Errorf("a reader listing the versions = %d, want 200: %s", code, body)
	}
	code, body := restore(t, reader, acmeHost, memo, 1, 2)
	if code != http.StatusForbidden {
		t.Errorf("a reader restoring a version = %d, want 403: %s", code, body)
	}
	if stored := w.versionsOf(memo); len(stored) != 2 {
		t.Errorf("the refused restore left %d versions, want the two the editor wrote", len(stored))
	}
	if n := w.outbox(rest.RestoredEvent); n != 0 {
		t.Errorf("the refused restore published %d %s rows, want none", n, rest.RestoredEvent)
	}
}
