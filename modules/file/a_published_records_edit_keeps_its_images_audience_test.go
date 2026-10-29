package file_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	pngimage "image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// shownRecord is a record with a public face: Shown is what its Spec answers
// for the audience, as content answers it from its published status.
type shownRecord struct {
	crud.Base
	Body  string `json:"body" ui:"widget:richtext"`
	Shown bool   `json:"shown"`
}

func (*shownRecord) TableName() string { return "shown_records" }

// TestAPublishedRecordsEditKeepsItsImagesAudience drives the ordinary edit door
// of a record with a public face, and asks the anonymous door after each write:
// two shown records sharing an image keep it public while either still shows it,
// it goes private when the last stops, and an image a shown record gains in an
// edit is served to anybody at once.
func TestAPublishedRecordsEditKeepsItsImagesAudience(t *testing.T) {
	admin, conn := dbtest.Schema(t, file.Migrations)
	if _, err := admin.ExecContext(t.Context(), `CREATE TABLE shown_records (
		id uuid PRIMARY KEY, tenant_id uuid NOT NULL,
		created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
		deleted_at timestamptz, body text NOT NULL, shown boolean NOT NULL
	);
	ALTER TABLE shown_records ENABLE ROW LEVEL SECURITY;
	ALTER TABLE shown_records FORCE ROW LEVEL SECURITY;
	CREATE POLICY shown_records_tenant ON shown_records
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`); err != nil {
		t.Fatal(err)
	}
	svc, mod := file.New(file.Deps{Storage: file.Local(t.TempDir())})
	who := caller{hosts: map[string]tenancy.Tenant{host: acme}}
	api, router := httpx.New(httpx.Options{
		SystemAccess: declaredCrossTenantAccess(),
		Cache:        cache.Memory("pkit"), PublicHost: host, Tenants: who, Conn: conn, Authorize: who,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	mod.Routes(surfacesOf(api))
	spec := rest.Spec[*shownRecord]{
		Module: "content", Entity: "shown", Path: "/shown",
		Read: "content:read", Write: "content:write",
		RichTextFiles: file.RichTextFiles{Opener: svc}, FileUses: file.RecordUses{Service: svc},
		Audience: func(_ context.Context, _ db.Tx[db.Tenant], r *shownRecord) (bool, error) {
			return r.Shown, nil
		},
	}
	spec.Mount(api.Surfaces("content"))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	request := func(method, at, payload string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, "http://"+host+at, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	uploadImage := func() uuid.UUID {
		t.Helper()
		var encoded bytes.Buffer
		if err := pngimage.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
			t.Fatal(err)
		}
		status, body := upload(t, router, files+"?image=true", "image.png", "image/png", encoded.String())
		var uploaded contracts.File
		if status != http.StatusCreated || json.Unmarshal([]byte(body), &uploaded) != nil || uploaded.ID == uuid.Nil {
			t.Fatalf("upload = %d %s", status, body)
		}
		return uploaded.ID
	}
	showing := func(images ...uuid.UUID) string {
		var body strings.Builder
		for _, id := range images {
			body.WriteString("![picture](pk-file:")
			body.WriteString(id.String())
			body.WriteString(")\n\n")
		}
		encoded, _ := json.Marshal(body.String())
		return string(encoded)
	}
	create := func(images ...uuid.UUID) string {
		t.Helper()
		status, body := request(http.MethodPost, "/api/v1/content/shown", `{"shown":true,"body":`+showing(images...)+`}`)
		var record shownRecord
		if status != http.StatusCreated || json.Unmarshal([]byte(body), &record) != nil || record.ID == uuid.Nil {
			t.Fatalf("create a shown record = %d %s", status, body)
		}
		return "/api/v1/content/shown/" + record.ID.String()
	}
	edit := func(item string, images ...uuid.UUID) {
		t.Helper()
		if status, body := request(http.MethodPatch, item, `{"body":`+showing(images...)+`}`); status != http.StatusOK {
			t.Fatalf("edit %s = %d %s, want 200", item, status, body)
		}
	}
	anonymous := func(id uuid.UUID) int {
		t.Helper()
		code, _, _ := send(t, router, http.MethodGet, public+id.String(), false)
		return code
	}

	shared, added := uploadImage(), uploadImage()
	first, second := create(shared), create(shared)
	if code := anonymous(shared); code != http.StatusOK {
		t.Fatalf("an image two shown records name, anonymously = %d, want 200", code)
	}

	edit(first)
	if code := anonymous(shared); code != http.StatusOK {
		t.Errorf("an image one shown record still names, after the other dropped it, anonymously = %d, want 200", code)
	}
	edit(second)
	if code := anonymous(shared); code != http.StatusNotFound {
		t.Errorf("an image no shown record names any more, anonymously = %d, want 404", code)
	}

	if code := anonymous(added); code != http.StatusNotFound {
		t.Fatalf("an image nothing names yet, anonymously = %d, want 404", code)
	}
	edit(first, added)
	if code := anonymous(added); code != http.StatusOK {
		t.Errorf("an image a shown record gained in an edit, anonymously = %d, want 200", code)
	}
}
