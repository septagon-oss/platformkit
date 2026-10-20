package internal

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/modules/picture/contracts/picturetest"
)

// A warm entry for one declaration must never answer for another declaration
// with the same value, and a refusal must carry neither a validator nor a
// storable cache policy.
func TestAWarmPictureFollowsTheTemplateItsSourceNames(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	_, err := admin.ExecContext(t.Context(), `CREATE TABLE picture_sources (tenant_id uuid NOT NULL, source_key text NOT NULL, owner_id uuid NOT NULL, readable boolean NOT NULL, colour text NOT NULL, PRIMARY KEY (tenant_id, source_key)); ALTER TABLE picture_sources ENABLE ROW LEVEL SECURITY; ALTER TABLE picture_sources FORCE ROW LEVEL SECURITY; CREATE POLICY picture_sources_tenant ON picture_sources USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));`)
	if err != nil {
		t.Fatal(err)
	}
	h := pictureHost{a: tenancy.Tenant{ID: uuid.New(), Slug: "a", Name: "A"}, b: tenancy.Tenant{ID: uuid.New(), Slug: "b", Name: "B"}}
	actor := uuid.New()
	for _, tenant := range []uuid.UUID{h.a.ID, h.b.ID} {
		if _, err := admin.ExecContext(t.Context(), `INSERT INTO picture_sources VALUES ($1,'shared',$2,$3,'#AABBCC')`, tenant, actor, tenant == h.a.ID); err != nil {
			t.Fatal(err)
		}
	}
	first, err := contracts.Compile("fixture", picturetest.SVG)
	if err != nil {
		t.Fatal(err)
	}
	second, err := contracts.Compile("fixture", strings.Replace(picturetest.SVG, `stroke-width="2"`, `stroke-width="3"`, 1))
	if err != nil {
		t.Fatal(err)
	}
	source := &sqlSource{digest: first.Digest()}
	api, router := httpx.New(httpx.Options{Unwired: true, Cache: cache.Memory("dry"), Installation: "a.test", PublicHost: "a.test", Tenants: h, Authorize: h, Entitle: h, Log: slog.New(slog.DiscardHandler), Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
		return tenancy.Principal{UserID: actor}, true, nil
	}})
	Register(api.Surfaces("picture").App, source, map[[32]byte]contracts.Template{first.Digest(): first, second.Digest(): second})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	if err := api.Connect(conn, cache.Memory("pictures")); err != nil {
		t.Fatal(err)
	}
	token, _ := entity.ComposedPicture("shared")
	path := strings.ReplaceAll(contracts.PictureSource, "{reference}", string(token))
	get := func(host string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
		r.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	warm := get("a.test")
	if warm.Code != http.StatusOK || warm.Body.String() != picturetest.Oracle {
		t.Fatalf("first declaration answered %d %s", warm.Code, warm.Body.String())
	}
	source.digest = second.Digest()
	changed := get("a.test")
	if changed.Code != http.StatusOK || !strings.Contains(changed.Body.String(), `stroke-width="3"`) {
		t.Fatalf("a warm entry answered for another declaration with the same value: %d %s", changed.Code, changed.Body.String())
	}
	if changed.Header().Get("ETag") == warm.Header().Get("ETag") {
		t.Fatal("two declarations share one validator")
	}
	refused := get("b.test")
	if refused.Code != http.StatusNotFound || refused.Header().Get("ETag") != "" || strings.Contains(refused.Body.String(), "<svg") {
		t.Fatalf("an unreadable source in another tenant answered %d %s", refused.Code, refused.Body.String())
	}
	if policy := refused.Header().Get("Cache-Control"); !strings.Contains(policy, "no-store") || strings.Contains(policy, "public") {
		t.Fatalf("a refused picture read is storable: Cache-Control %q", policy)
	}
}
