package file_test

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
)

// countedCover is a picture-bearing record whose writes are counted, so a PATCH
// can quote the revision it read.
type countedCover struct {
	crud.Base
	Title    string     `json:"title"`
	Cover    *uuid.UUID `json:"cover,omitempty" ui:"present:image"`
	Revision int64      `json:"revision" gorm:"not null;default:1" readOnly:"true" required:"false"`
}

func (*countedCover) TableName() string { return "counted_covers" }

// patchIf sends a PATCH quoting the given ETag, or none when tag is empty.
func patchIf(t *testing.T, r http.Handler, at, tag, payload string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "http://"+host+at, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if tag != "" {
		req.Header.Set("If-Match", tag)
	}
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestTwoCoverWritesLeaveOneUseForTheCoverTheRowShows races two PATCHes naming
// different covers on one row: whichever lands last, the ledger holds a live use
// for the cover the row shows and none for the one it does not.
func TestTwoCoverWritesLeaveOneUseForTheCoverTheRowShows(t *testing.T) {
	router, admin, _ := coveredMount(t)
	a := uploadAs(t, router, host, files+"?image=true", "a.png", "image/png", pngBytes(t))
	b := uploadAs(t, router, host, files+"?image=true", "b.png", "image/png", pngBytes(t))
	code, body := writeJSON(t, router, http.MethodPost, "/api/v1/content/covered", `{"title":"mine"}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}
	id := field(t, body, "id")

	for round := range 5 {
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for i, cover := range []string{a, b} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[i], _ = patchIf(t, router, "/api/v1/content/covered/"+id, "", `{"cover":"`+cover+`"}`)
			}()
		}
		wg.Wait()
		if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
			t.Fatalf("round %d: two cover PATCHes answered %v, want both 200", round, codes)
		}
		var shown string
		if err := admin.QueryRowContext(t.Context(), "SELECT cover::text FROM covered_records WHERE id = $1", id).Scan(&shown); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{a, b} {
			want := 0
			if f == shown {
				want = 1
			}
			if n := count(t, admin, "SELECT count(*) FROM file_uses WHERE file_id = $1 AND record = $2", f, id); n != want {
				t.Errorf("round %d: row shows %s; file %s has %d uses, want %d", round, shown, f, n, want)
			}
		}
	}
}

// TestAStaleCoverWriteFilesNothing quotes a revision the row has passed: the
// PATCH is refused with 412, the row keeps its cover, the ledger keeps its use,
// and no update event is written.
func TestAStaleCoverWriteFilesNothing(t *testing.T) {
	routerC, admin, spec := countedCoverMount(t)
	a := uploadAs(t, routerC, host, files+"?image=true", "a.png", "image/png", pngBytes(t))
	b := uploadAs(t, routerC, host, files+"?image=true", "b.png", "image/png", pngBytes(t))

	code, body := writeJSON(t, routerC, http.MethodPost, "/api/v1/content/counted", `{"title":"mine","cover":"`+a+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}
	id := field(t, body, "id")
	if code, body := patchIf(t, routerC, "/api/v1/content/counted/"+id, `"1"`, `{"title":"moved"}`); code != http.StatusOK {
		t.Fatalf("PATCH at revision 1 = %d %s", code, body)
	}
	events := count(t, admin, "SELECT count(*) FROM "+outbox+" WHERE name = $1", spec.Event(rest.Updated))

	code, body = patchIf(t, routerC, "/api/v1/content/counted/"+id, `"1"`, `{"cover":"`+b+`"}`)
	if code != http.StatusPreconditionFailed {
		t.Fatalf("PATCH quoting a passed revision = %d %s, want 412", code, body)
	}
	if n := count(t, admin, "SELECT count(*) FROM counted_covers WHERE id = $1 AND cover = $2", id, a); n != 1 {
		t.Errorf("a refused PATCH moved the row's cover")
	}
	if n := count(t, admin, "SELECT count(*) FROM file_uses WHERE file_id = $1 AND record = $2", a, id); n != 1 {
		t.Errorf("a refused PATCH left %d uses of the cover the row still shows, want 1", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM file_uses WHERE file_id = $1", b); n != 0 {
		t.Errorf("a refused PATCH filed %d uses of the cover it named", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM "+outbox+" WHERE name = $1", spec.Event(rest.Updated)); n != events {
		t.Errorf("a refused PATCH emitted %d update events", n-events)
	}
}

// renamedCover is a picture field whose wire name is not its column, the shape
// a module's own cover takes.
type renamedCover struct {
	crud.Base
	Title        string     `json:"title"`
	CoverImageID *uuid.UUID `json:"coverImageId,omitempty" gorm:"column:cover_image_id;type:uuid" ui:"present:image"`
}

func (*renamedCover) TableName() string { return "renamed_covers" }

// TestACoverWhoseNameIsNotItsColumnFilesAndEndsItsUse follows a cover named
// `coverImageId` on the wire and `cover_image_id` in the table through a create,
// a PATCH of another field, a PATCH that clears it and one that names it again.
func TestACoverWhoseNameIsNotItsColumnFilesAndEndsItsUse(t *testing.T) {
	router, admin, _ := pictureMount(t, "renamed_covers", "cover_image_id uuid",
		rest.Spec[*renamedCover]{Module: "content", Entity: "renamed", Path: "/renamed",
			Read: "content:read", Write: "content:write"})
	cover := uploadAs(t, router, host, files+"?image=true", "cover.png", "image/png", pngBytes(t))
	uses := func() int {
		return count(t, admin, "SELECT count(*) FROM file_uses WHERE file_id = $1 AND field = 'cover_image_id'", cover)
	}
	code, body := writeJSON(t, router, http.MethodPost, "/api/v1/content/renamed", `{"title":"mine","coverImageId":"`+cover+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}
	id := field(t, body, "id")
	for _, step := range []struct {
		patch string
		want  int
	}{
		{"", 1},
		{`{"title":"renamed"}`, 1},
		{`{"coverImageId":null}`, 0},
		{`{"coverImageId":"` + cover + `"}`, 1},
	} {
		if step.patch != "" {
			if code, body := patchIf(t, router, "/api/v1/content/renamed/"+id, "", step.patch); code != http.StatusOK {
				t.Fatalf("PATCH %s = %d %s", step.patch, code, body)
			}
		}
		if n := uses(); n != step.want {
			t.Errorf("after %q the cover has %d uses, want %d", step.patch, n, step.want)
		}
	}
}

// countedCoverMount is coveredMount over the counted record.
func countedCoverMount(t *testing.T) (chi.Router, *sql.DB, rest.Spec[*countedCover]) {
	return pictureMount(t, "counted_covers", "cover uuid, revision bigint NOT NULL DEFAULT 1",
		rest.Spec[*countedCover]{Module: "content", Entity: "counted", Path: "/counted",
			Read: "content:read", Write: "content:write"})
}

// pictureMount is coveredMount for any picture-bearing record: the file module
// and the given resource over two tenants, its table made of the base columns, a
// title and the given extra columns, with the file ledger wired.
func pictureMount[T crud.Entity](t *testing.T, table, columns string, spec rest.Spec[T]) (chi.Router, *sql.DB, rest.Spec[T]) {
	t.Helper()
	admin, conn := dbtest.Schema(t, file.Migrations)
	if _, err := admin.ExecContext(t.Context(), `CREATE TABLE `+table+` (
		id uuid PRIMARY KEY, tenant_id uuid NOT NULL,
		created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
		deleted_at timestamptz, title text NOT NULL DEFAULT '', `+columns+`
	);
	ALTER TABLE `+table+` ENABLE ROW LEVEL SECURITY;
	ALTER TABLE `+table+` FORCE ROW LEVEL SECURITY;
	CREATE POLICY `+table+`_tenant ON `+table+`
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`); err != nil {
		t.Fatal(err)
	}
	svc, mod := file.New(file.Deps{Storage: file.Local(t.TempDir())})
	who := caller{hosts: map[string]tenancy.Tenant{host: acme, otherHost: globex}}
	api, router := httpx.New(httpx.Options{
		SystemAccess: declaredCrossTenantAccess(),
		Cache:        cache.Memory("pkit"), PublicHost: host, Tenants: who, Conn: conn, Authorize: who,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	mod.Routes(surfacesOf(api))
	spec.FileUses = file.RecordUses{Service: svc}
	spec.Mount(api.Surfaces(spec.Module))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	return router, admin, spec
}
