package file_test

import (
	"bytes"
	"context"
	"database/sql"
	"image"
	pngimage "image/png"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
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

// coveredRecord is a record with one `present:image` field, the shape the
// catalogue tells a shell to fetch through the file module's door.
type coveredRecord struct {
	crud.Base
	Title string     `json:"title"`
	Cover *uuid.UUID `json:"cover,omitempty" ui:"present:image"`
}

func (*coveredRecord) TableName() string { return "covered_records" }

// coveredMount is the file module and one picture-bearing resource, over two
// tenants, with the file ledger wired as the reference composition wires it.
func coveredMount(t *testing.T) (chi.Router, *sql.DB, rest.Spec[*coveredRecord]) {
	t.Helper()
	admin, conn := dbtest.Schema(t, file.Migrations)
	if _, err := admin.ExecContext(t.Context(), `CREATE TABLE covered_records (
		id uuid PRIMARY KEY, tenant_id uuid NOT NULL,
		created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
		deleted_at timestamptz, title text NOT NULL DEFAULT '', cover uuid
	);
	ALTER TABLE covered_records ENABLE ROW LEVEL SECURITY;
	ALTER TABLE covered_records FORCE ROW LEVEL SECURITY;
	CREATE POLICY covered_records_tenant ON covered_records
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
	spec := rest.Spec[*coveredRecord]{
		Module: "content", Entity: "covered", Path: "/covered",
		Read: "content:read", Write: "content:write",
		FileUses: file.RecordUses{Service: svc},
	}
	spec.Mount(api.Surfaces("content"))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	return router, admin, spec
}

// uploadAs posts one file from the named origin, so a second tenant can own one.
func uploadAs(t *testing.T, r http.Handler, from, at, name, contentType string, body []byte) string {
	t.Helper()
	var form bytes.Buffer
	w := multipart.NewWriter(&form)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+name+`"`)
	header.Set("Content-Type", contentType)
	part, err := w.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://"+from+at, &form)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload %s from %s = %d %s", name, from, rec.Code, rec.Body.String())
	}
	return field(t, rec.Body.String(), "id")
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := pngimage.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func writeJSON(t *testing.T, r http.Handler, method, at, payload string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+at, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func count(t *testing.T, admin *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := admin.QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestAPictureFieldRefusesAnotherTenantsFile names a file the writing tenant
// cannot see: the write is refused, and no row, use or event is left behind.
func TestAPictureFieldRefusesAnotherTenantsFile(t *testing.T) {
	router, admin, spec := coveredMount(t)
	foreign := uploadAs(t, router, otherHost, files+"?image=true", "theirs.png", "image/png", pngBytes(t))

	code, body := writeJSON(t, router, http.MethodPost, "/api/v1/content/covered", `{"title":"mine","cover":"`+foreign+`"}`)
	if code < 400 || code >= 500 {
		t.Fatalf("create naming another tenant's file = %d %s, want a 4xx refusal", code, body)
	}
	if n := count(t, admin, "SELECT count(*) FROM covered_records"); n != 0 {
		t.Errorf("a refused create left %d rows", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM file_uses WHERE file_id = $1", foreign); n != 0 {
		t.Errorf("a refused create filed %d uses of another tenant's file", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM "+outbox+" WHERE name = $1", spec.Event(rest.Created)); n != 0 {
		t.Errorf("a refused create emitted %d events", n)
	}
}

// TestAPictureFieldFilesItsUseAndEndsIt follows one cover through a create, a
// PATCH that leaves it alone, a PATCH that clears it and a delete.
func TestAPictureFieldFilesItsUseAndEndsIt(t *testing.T) {
	router, admin, _ := coveredMount(t)
	cover := uploadAs(t, router, host, files+"?image=true", "cover.png", "image/png", pngBytes(t))
	uses := func() int {
		return count(t, admin, "SELECT count(*) FROM file_uses WHERE file_id = $1 AND field = 'cover'", cover)
	}

	code, body := writeJSON(t, router, http.MethodPost, "/api/v1/content/covered", `{"title":"mine","cover":"`+cover+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("create with a cover = %d %s", code, body)
	}
	id := field(t, body, "id")
	if n := uses(); n != 1 {
		t.Fatalf("a create with a cover filed %d uses, want 1", n)
	}
	if code, body := writeJSON(t, router, http.MethodPatch, "/api/v1/content/covered/"+id, `{"title":"renamed"}`); code != http.StatusOK {
		t.Fatalf("PATCH the title = %d %s", code, body)
	}
	if n := uses(); n != 1 {
		t.Errorf("a PATCH that did not name the cover left %d uses, want 1", n)
	}
	if code, body := writeJSON(t, router, http.MethodPatch, "/api/v1/content/covered/"+id, `{"cover":null}`); code != http.StatusOK {
		t.Fatalf("PATCH the cover away = %d %s", code, body)
	}
	if n := uses(); n != 0 {
		t.Errorf("a PATCH that cleared the cover left %d uses, want 0", n)
	}
	if code, body := writeJSON(t, router, http.MethodPatch, "/api/v1/content/covered/"+id, `{"cover":"`+cover+`"}`); code != http.StatusOK {
		t.Fatalf("PATCH the cover back = %d %s", code, body)
	}
	if code, body, _ := send(t, router, http.MethodDelete, "/api/v1/content/covered/"+id, true); code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", code, body)
	}
	if n := uses(); n != 0 {
		t.Errorf("a deleted record left %d uses of its cover", n)
	}
}
