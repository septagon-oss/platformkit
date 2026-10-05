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

type imageRecord struct {
	crud.Base
	Body string `json:"body" ui:"widget:richtext"`
}

func (*imageRecord) TableName() string { return "image_records" }

func TestDeletingARecordEndsUsesEvenWhenItsImageWasRemoved(t *testing.T) {
	for _, removeImage := range []bool{false, true} {
		name := "image still present"
		if removeImage {
			name = "image removed first"
		}
		t.Run(name, func(t *testing.T) {
			admin, conn := dbtest.Schema(t, file.Migrations)
			_, err := admin.ExecContext(t.Context(), `CREATE TABLE image_records (
				id uuid PRIMARY KEY, tenant_id uuid NOT NULL,
				created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
				deleted_at timestamptz, body text NOT NULL
			);
			ALTER TABLE image_records ENABLE ROW LEVEL SECURITY;
			ALTER TABLE image_records FORCE ROW LEVEL SECURITY;
			CREATE POLICY image_records_tenant ON image_records
			USING (platformkit_tenant_match(tenant_id))
			WITH CHECK (platformkit_tenant_match(tenant_id));`)
			if err != nil {
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
			var encoded bytes.Buffer
			if err := pngimage.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
				t.Fatal(err)
			}
			status, body := upload(t, router, files+"?image=true", "image.png", "image/png", encoded.String())
			if status != http.StatusCreated {
				t.Fatalf("upload = %d %s", status, body)
			}
			var uploaded contracts.File
			if err := json.Unmarshal([]byte(body), &uploaded); err != nil || uploaded.ID == uuid.Nil {
				t.Fatalf("upload response = %s: %v", body, err)
			}
			payload := `{"body":"![chart](pk-file:` + uploaded.ID.String() + `)"}`
			req := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/v1/content/images", strings.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != http.StatusCreated {
				t.Fatalf("create with an image = %d %s", response.Code, response.Body.String())
			}
			var record imageRecord
			if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil || record.ID == uuid.Nil {
				t.Fatalf("created record = %s: %v", response.Body.String(), err)
			}
			if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				uses, err := svc.Uses(ctx, tx, uploaded.ID)
				if err == nil && (len(uses) != 1 || uses[0].Use.Record != record.ID) {
					t.Errorf("uses = %+v, want created record %s", uses, record.ID)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if removeImage {
				status, body, _ = send(t, router, http.MethodDelete, files+"/"+uploaded.ID.String(), true)
				// Refusing deletion of a used image also preserves the lifecycle.
				if status != http.StatusNoContent && status != http.StatusConflict {
					t.Fatalf("delete image = %d %s, want deleted or refused as in use", status, body)
				}
				t.Logf("delete image = %d", status)
			}
			status, body, _ = send(t, router, http.MethodDelete, "/api/v1/content/images/"+record.ID.String(), true)
			if status != http.StatusNoContent {
				t.Errorf("delete existing record = %d %s, want 204 even after its image was removed", status, body)
			}
			for table, condition := range map[string]string{
				"image_records": "id", "file_uses": "record",
			} {
				var remaining int
				if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table+" WHERE "+condition+" = $1", record.ID).Scan(&remaining); err != nil {
					t.Fatal(err)
				}
				if remaining != 0 {
					t.Errorf("%s retains %d rows for deleted record, want none", table, remaining)
				}
			}
			var emitted int
			if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = $1", spec.Event(rest.Deleted)).Scan(&emitted); err != nil {
				t.Fatal(err)
			}
			if emitted != 1 {
				t.Errorf("record deletion emitted %d events, want one", emitted)
			}
		})
	}
}
