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
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestAnEditThatDropsARemovedImageEndsItsUse is the update door of the rule the
// delete door already keeps: a body that stops naming an image whose file was
// removed is saved, and the ledger row for that image ends with it. Another
// tenant's edit of the same record reaches nothing and leaves the row alone.
func TestAnEditThatDropsARemovedImageEndsItsUse(t *testing.T) {
	admin, conn := dbtest.Schema(t, file.Migrations)
	if _, err := admin.ExecContext(t.Context(), `CREATE TABLE image_records (
		id uuid PRIMARY KEY, tenant_id uuid NOT NULL,
		created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
		deleted_at timestamptz, body text NOT NULL
	);
	ALTER TABLE image_records ENABLE ROW LEVEL SECURITY;
	ALTER TABLE image_records FORCE ROW LEVEL SECURITY;
	CREATE POLICY image_records_tenant ON image_records
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`); err != nil {
		t.Fatal(err)
	}
	svc, mod := file.Module(file.Deps{Storage: file.Local(t.TempDir())})
	who := caller{hosts: map[string]tenancy.Tenant{host: acme, otherHost: globex}}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: who, Conn: conn, Authorize: who,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	mod.Routes(surfacesOf(api))
	spec := rest.Spec[*imageRecord]{
		Module: "content", Entity: "image", Path: "/images",
		Read: "content:read", Write: "content:write",
		RichTextFiles: file.RichTextFiles{Opener: svc}, FileUses: file.RecordUses{Service: svc},
	}
	spec.Mount(api.Surfaces("content"))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	request := func(from, method, at, payload string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, "http://"+from+at, strings.NewReader(payload))
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
	ledger := func(record uuid.UUID) []uuid.UUID {
		t.Helper()
		rows, err := admin.QueryContext(t.Context(), `SELECT file_id FROM file_uses WHERE record = $1 ORDER BY file_id`, record)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return ids
	}

	kept, removed := uploadImage(), uploadImage()
	status, body := request(host, http.MethodPost, "/api/v1/content/images",
		`{"body":"![kept](pk-file:`+kept.String()+`)\n\n![removed](pk-file:`+removed.String()+`)"}`)
	var record imageRecord
	if status != http.StatusCreated || json.Unmarshal([]byte(body), &record) != nil || record.ID == uuid.Nil {
		t.Fatalf("create with two images = %d %s", status, body)
	}
	if got := ledger(record.ID); len(got) != 2 {
		t.Fatalf("ledger after create = %v, want both images", got)
	}
	if status, body, _ := send(t, router, http.MethodDelete, files+"/"+removed.String(), true); status != http.StatusNoContent && status != http.StatusConflict {
		t.Fatalf("delete image = %d %s, want deleted or refused as in use", status, body)
	}

	item := "/api/v1/content/images/" + record.ID.String()
	edit := `{"body":"![kept](pk-file:` + kept.String() + `)"}`
	if status, _ := request(otherHost, http.MethodPatch, item, edit); status != http.StatusNotFound {
		t.Errorf("another tenant's edit = %d, want 404", status)
	}
	if got := ledger(record.ID); len(got) != 2 {
		t.Errorf("ledger after another tenant's edit = %v, want both rows untouched", got)
	}

	if status, body := request(host, http.MethodPatch, item, edit); status != http.StatusOK {
		t.Fatalf("edit dropping the removed image = %d %s, want 200", status, body)
	}
	if got := ledger(record.ID); len(got) != 1 || got[0] != kept {
		t.Errorf("ledger after the edit = %v, want only %s", got, kept)
	}
	var saved string
	if err := admin.QueryRowContext(t.Context(), `SELECT body FROM image_records WHERE id = $1`, record.ID).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(saved, removed.String()) || !strings.Contains(saved, kept.String()) {
		t.Errorf("saved body = %q, want the kept image only", saved)
	}
}
