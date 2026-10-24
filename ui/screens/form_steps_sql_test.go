package screens_test

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/screens"
)

type steppedRow struct {
	entity.Base
	Title    string   `json:"title" validate:"required"`
	Body     string   `json:"body,omitempty" gorm:"type:text" ui:"widget:textarea;section:details"`
	Rank     int64    `json:"rank" ui:"section:details"`
	Pinned   bool     `json:"pinned" ui:"section:options"`
	Tags     []string `json:"tags,omitempty" gorm:"-" ui:"section:options"`
	Status   string   `json:"status"`
	Revision int64    `json:"revision" readOnly:"true" required:"false" gorm:"not null;default:1"`
}

func (steppedRow) TableName() string { return "notes" }

type stepAccess struct{ denied atomic.Bool }

func (a *stepAccess) ByHost(ctx context.Context, tx db.Tx[db.System], host string) (tenancy.Tenant, error) {
	tenant, err := (formDirectory{}).ByHost(ctx, tx, host)
	if strings.HasPrefix(host, "other.") {
		tenant.ID = uuid.MustParse("33333333-3333-4333-8333-333333333333")
	}
	return tenant, err
}
func (a *stepAccess) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return !a.denied.Load(), nil
}

func sqlStepServer(t *testing.T, fail *atomic.Bool) (http.Handler, *sql.DB, *stepAccess) {
	t.Helper()
	admin, conn := dbtest.Schema(t)
	_, err := admin.ExecContext(t.Context(), `CREATE TABLE notes (
	 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, created_at timestamptz NOT NULL,
	 updated_at timestamptz NOT NULL, deleted_at timestamptz, title text NOT NULL,
	 body text NOT NULL DEFAULT '', status text NOT NULL DEFAULT 'open', rank bigint NOT NULL DEFAULT 0,
	 pinned boolean NOT NULL DEFAULT false, revision bigint NOT NULL DEFAULT 1);
	 ALTER TABLE notes ENABLE ROW LEVEL SECURITY; ALTER TABLE notes FORCE ROW LEVEL SECURITY;
	 CREATE POLICY notes_tenant ON notes USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));`)
	if err != nil {
		t.Fatal(err)
	}
	access := &stepAccess{}
	api, router := httpx.New(httpx.Options{Cache: cache.Memory("pkit"), PublicHost: formHost, Tenants: access, Authorize: access, Conn: conn, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.MustParse("22222222-2222-4222-8222-222222222222")}, true, nil
		}})
	fixture := steppedNote()
	fixture.Present.Forms.Update = fixture.Present.Forms.Create
	spec := rest.Spec[*steppedRow]{Module: "note", Entity: "note", Path: "/notes", Read: "note:read", Write: "note:write", Immutable: []string{"status"}, Present: fixture.Present,
		AfterCreate: func(context.Context, db.Tx[db.Tenant], *steppedRow) error {
			if fail.Load() {
				return problem.New(422, "The note cannot be saved")
			}
			return nil
		}}
	spec.Mount(api.Surfaces("note"))
	r := api.Resources()[0]
	screens.Mount(api.Surfaces("note").App, formShell(), opts, r)
	mountSingletonSteps(api, fixture.Present)
	return router, admin, access
}

func mountSingletonSteps(api *httpx.API, hints entity.EntryHints) {
	hints.Forms = &entity.FormsHints{Update: hints.Forms.Update}
	s := rest.Singleton[*steppedRow]{Module: "settings_owner", Entity: "note", Path: "/settings", Read: "note:read", Write: "note:write", Present: hints,
		Load: func(_ context.Context, tx db.Tx[db.Tenant]) (*steppedRow, error) {
			var row steppedRow
			if err := tx.DB().Take(&row).Error; err != nil {
				return nil, err
			}
			return &row, nil
		},
		Save: func(ctx context.Context, tx db.Tx[db.Tenant], row *steppedRow) (*steppedRow, error) {
			row.Revision++
			return row, crud.Update(ctx, tx, row, "title", "body", "rank", "pinned", "revision")
		}}
	s.Mount(api.Surfaces("settings_owner"))
	r := api.Resources()[len(api.Resources())-1]
	screens.Mount(api.Surfaces("settings_owner").App, formShell(), opts, r)
}

func TestSingletonStepFormRefusesTheOriginalRevisionAfterSave(t *testing.T) {
	var fail atomic.Bool
	h, admin, _ := sqlStepServer(t, &fail)
	s := readyNote(t, h, "/app/note/notes/new", "Initial")
	if out := postStep(h, "/app/note/notes", s, "submit", nil); out.Code != 303 {
		t.Fatal(out.Code, out.Body)
	}
	at := "/app/settings_owner/settings"
	s = readyNote(t, h, at+"/edit", "Updated")
	if out := postStep(h, at, s, "submit", nil); out.Code != 303 {
		t.Fatalf("singleton save: %d %s", out.Code, out.Body)
	}
	if out := postStep(h, at, s, "submit", nil); out.Code != 412 {
		t.Fatalf("stale singleton: %d %s", out.Code, out.Body)
	}
	var revision int
	if err := admin.QueryRowContext(t.Context(), "SELECT revision FROM notes").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != 2 {
		t.Fatalf("stale singleton wrote revision %d", revision)
	}
}

func TestSteppedUpdatesSerializeOriginalRevisionAndKeepTenantIsolation(t *testing.T) {
	var fail atomic.Bool
	h, admin, access := sqlStepServer(t, &fail)
	at := "/app/note/notes"
	s := readyNote(t, h, at+"/new", "Notebook")
	s.Answers["rank"] = "9007199254740993"
	created := postStep(h, at, s, "submit", nil)
	if created.Code != 303 {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	item := created.Header().Get("Location")
	initial := continuation(t, stepRequest(h, item+"/edit", nil), 200, 0)
	if initial.Answers["rank"] != "9007199254740993" {
		t.Fatalf("edit rounded a stored integer: %s", initial.Answers["rank"])
	}
	s = readyNote(t, h, item+"/edit", "Notebook")
	if s.Revision != "1" {
		t.Fatalf("initial revision: %+v", s)
	}
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, title := range []string{"First", "Second"} {
		state := s
		state.Answers = map[string]string{"title": title, "body": "changed", "rank": "8"}
		go func() { <-start; results <- postStep(h, item, state, "submit", nil).Code }()
	}
	close(start)
	a, b := <-results, <-results
	if !(a == 303 && b == 412 || a == 412 && b == 303) {
		t.Fatalf("competing revisions: %d %d", a, b)
	}
	var revision, events int
	if err := admin.QueryRowContext(t.Context(), "SELECT revision FROM notes").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if revision != 2 || events != 2 {
		t.Fatalf("race wrote extra state: revision=%d events=%d", revision, events)
	}
	access.denied.Store(true)
	if out := postStep(h, item, s, "submit", nil); out.Code != 403 {
		t.Fatalf("revoked grant: %d", out.Code)
	}
	access.denied.Store(false)
	foreign := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "other.localhost"
		r.Header.Set("Origin", "http://other.localhost")
		h.ServeHTTP(w, r)
	})
	if out := stepRequest(foreign, item+"/edit", nil); out.Code != 404 {
		t.Fatalf("foreign read: %d %s", out.Code, out.Body)
	}
	if out := postStep(foreign, item, s, "submit", nil); out.Code != 404 {
		t.Fatalf("foreign write: %d %s", out.Code, out.Body)
	}
	fail.Store(true)
	fresh := readyNote(t, h, at+"/new", "Rolled back")
	refused := postStep(h, at, fresh, "submit", nil)
	continuation(t, refused, 422, 2)
	var rows int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM notes").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || events != 2 {
		t.Fatalf("refusal committed row/outbox: %d/%d", rows, events)
	}
}

func readyNote(t *testing.T, h http.Handler, initial, title string) rest.FormState {
	t.Helper()
	s := continuation(t, stepRequest(h, initial, nil), 200, 0)
	s = continuation(t, postStep(h, initial+"/steps", s, "next", url.Values{"title": {title}}), 200, 1)
	return continuation(t, postStep(h, initial+"/steps", s, "next", url.Values{"body": {"draft"}, "rank": {"7"}}), 200, 2)
}
