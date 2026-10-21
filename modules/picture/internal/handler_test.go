package internal

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

type pictureHost struct{ a, b tenancy.Tenant }

func (h pictureHost) ByHost(_ context.Context, _ db.Tx[db.System], host string) (tenancy.Tenant, error) {
	switch host {
	case "a.test":
		return h.a, nil
	case "b.test":
		return h.b, nil
	}
	return tenancy.Tenant{}, tenancy.ErrNoSuchHost
}

func (pictureHost) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return false, nil
}
func (pictureHost) Includes(context.Context, tenancy.Tenant, string) (bool, error) { return true, nil }

type sqlSource struct {
	digest    [32]byte
	calls     atomic.Int32
	err       error
	afterRead func()
}

func (s *sqlSource) Load(ctx context.Context, tx db.Tx[db.Tenant], key string) (contracts.Input, error) {
	s.calls.Add(1)
	if s.err != nil {
		return contracts.Input{}, s.err
	}
	tenant, ok := tenancy.FromContext(ctx)
	if !ok || tenant.ID != db.TenantOf(tx).ID {
		return contracts.Input{}, errors.New("scope mismatch")
	}
	actor, ok := tenancy.ActorFrom(ctx)
	if !ok {
		return contracts.Input{}, contracts.ErrUnavailable
	}
	var colour string
	err := tx.DB().Raw(`SELECT colour FROM picture_sources WHERE source_key = ? AND owner_id = ? AND readable FOR SHARE`, key, actor).Row().Scan(&colour)
	if errors.Is(err, sql.ErrNoRows) {
		return contracts.Input{}, contracts.ErrUnavailable
	}
	if err != nil {
		return contracts.Input{}, contracts.ErrTemporary
	}
	if s.afterRead != nil {
		s.afterRead()
	}
	v := picturetest.Value()
	v.Colours[0] = colour
	return contracts.Input{TemplateDigest: s.digest, Value: v}, nil
}

type countedCache struct {
	cache.Cache
	calls atomic.Int32
}

func (c *countedCache) Get(ctx context.Context, key cache.Key) ([]byte, bool, cache.Generation, error) {
	if key.Scope().Namespace() == "picture" {
		c.calls.Add(1)
	}
	return c.Cache.Get(ctx, key)
}

func TestDeliveryAuthorizesBeforeWarmBytesAndHTTPValidators(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	_, err := admin.ExecContext(t.Context(), `CREATE TABLE picture_sources (tenant_id uuid NOT NULL, source_key text NOT NULL, owner_id uuid NOT NULL, readable boolean NOT NULL, colour text NOT NULL, PRIMARY KEY (tenant_id, source_key)); ALTER TABLE picture_sources ENABLE ROW LEVEL SECURITY; ALTER TABLE picture_sources FORCE ROW LEVEL SECURITY; CREATE POLICY picture_sources_tenant ON picture_sources USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));`)
	if err != nil {
		t.Fatal(err)
	}
	h := pictureHost{a: tenancy.Tenant{ID: uuid.New(), Slug: "a", Name: "A"}, b: tenancy.Tenant{ID: uuid.New(), Slug: "b", Name: "B"}}
	actor, other := uuid.New(), uuid.New()
	for _, row := range []struct {
		tenant, owner uuid.UUID
		key           string
	}{{h.a.ID, actor, "shared"}, {h.b.ID, other, "only-b"}, {h.a.ID, other, "private"}} {
		if _, err := admin.ExecContext(t.Context(), `INSERT INTO picture_sources VALUES ($1,$2,$3,true,'#AABBCC')`, row.tenant, row.key, row.owner); err != nil {
			t.Fatal(err)
		}
	}
	template, err := contracts.Compile("fixture", picturetest.SVG)
	if err != nil {
		t.Fatal(err)
	}
	source := &sqlSource{digest: template.Digest()}
	store := &countedCache{Cache: cache.Memory("pictures")}
	dry := &countedCache{Cache: cache.Memory("dry")}
	api, router := httpx.New(httpx.Options{Unwired: true, Cache: dry, Installation: "a.test", PublicHost: "a.test", Tenants: h, Authorize: h, Entitle: h, Log: slog.New(slog.DiscardHandler), Authenticate: func(_ context.Context, tx db.Tx[db.Tenant], r *http.Request) (tenancy.Principal, bool, error) {
		who := actor
		if r.Header.Get("X-Actor") == "other" {
			who = other
		}
		p := tenancy.Principal{UserID: who}
		if r.Header.Get("X-Key") == "scoped" {
			p.TokenID = uuid.New()
			p.Permissions = []string{}
		}
		return p, true, nil
	}})
	Register(api.Surfaces("picture").App, source, map[[32]byte]contracts.Template{template.Digest(): template})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	if source.calls.Load() != 0 || dry.calls.Load() != 0 {
		t.Fatal("dry mount performed IO")
	}
	if _, ok := httpx.CacheFrom(t.Context()); ok {
		t.Fatal("cache outside request")
	}
	if err := api.Connect(conn, store); err != nil {
		t.Fatal(err)
	}
	token, _ := entity.ComposedPicture("shared")
	path := strings.ReplaceAll(contracts.PictureSource, "{reference}", string(token))
	request := func(method, host, path string, headers map[string]string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://"+host+path, nil)
		if authenticated {
			r.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
		}
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	first := request("GET", "a.test", path, nil, true)
	if first.Code != 200 || first.Body.String() != picturetest.Oracle {
		t.Fatalf("GET %d %s", first.Code, first.Body.String())
	}
	for k, want := range map[string]string{"Cache-Control": "private, no-store", "Content-Type": "image/svg+xml; charset=utf-8", "Content-Disposition": "inline", "X-Content-Type-Options": "nosniff", "Cross-Origin-Resource-Policy": "same-site", "Content-Security-Policy": "default-src 'none'; sandbox", "ETag": `"2ab9f69bb3f58230f64771b7fdb0a55cee88c9536bde570a2546fd4be6e63fb1"`} {
		if first.Header().Get(k) != want {
			t.Errorf("%s = %q", k, first.Header().Get(k))
		}
	}
	tag := first.Header().Get("ETag")
	for _, c := range []struct {
		method  string
		headers map[string]string
		status  int
		body    string
	}{
		{"HEAD", nil, 200, ""}, {"GET", map[string]string{"If-None-Match": tag}, 304, ""}, {"GET", map[string]string{"If-None-Match": "W/" + tag}, 304, ""}, {"GET", map[string]string{"If-None-Match": `"other", ` + tag}, 304, ""}, {"GET", map[string]string{"If-None-Match": "*"}, 304, ""},
		{"GET", map[string]string{"Range": "bytes=0-3"}, 206, "<svg"}, {"GET", map[string]string{"Range": "bytes=999999-"}, 416, ""}, {"GET", map[string]string{"If-Match": `"old"`}, 412, ""},
	} {
		before := source.calls.Load()
		w := request(c.method, "a.test", path, c.headers, true)
		if w.Code != c.status || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("%s %v = %d %s", c.method, c.headers, w.Code, w.Body.String())
		}
		if c.status != 416 && w.Body.String() != c.body {
			t.Errorf("body %q", w.Body.String())
		}
		if source.calls.Load() != before+1 {
			t.Fatal("validator bypassed source")
		}
	}
	before := source.calls.Load()
	if w := request("GET", "a.test", path, nil, false); w.Code != 401 {
		t.Fatalf("anonymous %d", w.Code)
	}
	if source.calls.Load() != before {
		t.Fatal("anonymous source call")
	}
	if w := request("GET", "a.test", path, map[string]string{"X-Key": "scoped"}, true); w.Code != 403 {
		t.Fatalf("scoped key %d %s", w.Code, w.Body.String())
	}
	for _, suffix := range []string{"?w=100", "?value=x", "?tenant=b", "?v=1"} {
		if w := request("GET", "a.test", path+suffix, nil, true); w.Code != 422 {
			t.Fatalf("query %s = %d", suffix, w.Code)
		}
	}
	if w := request("GET", "a.test", strings.Replace(path, string(token), "c1.eB", 1), nil, true); w.Code != 422 {
		t.Fatalf("invalid reference %d", w.Code)
	}
	if w := request("GET", "b.test", path, nil, true); w.Code != 404 {
		t.Fatalf("wrong tenant %d", w.Code)
	}
	if w := request("GET", "a.test", path, map[string]string{"X-Actor": "other"}, true); w.Code != 404 {
		t.Fatalf("wrong person %d", w.Code)
	}
	// Revocation commits in the same tenant authority before the warm conditional read.
	ctx := tenancy.WithTenant(t.Context(), h.a)
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`UPDATE picture_sources SET readable=false WHERE source_key='shared'`).Error
	}); err != nil {
		t.Fatal(err)
	}
	cacheCalls := store.calls.Load()
	for _, headers := range []map[string]string{{"If-None-Match": tag}, {"If-None-Match": "*"}, {"Range": "bytes=0-3"}, {"If-Match": tag}} {
		w := request("GET", "a.test", path, headers, true)
		if w.Code != 404 || w.Header().Get("ETag") != "" || strings.Contains(w.Body.String(), "<svg") {
			t.Fatalf("revoked %d %s", w.Code, w.Body.String())
		}
	}
	if store.calls.Load() != cacheCalls {
		t.Fatal("revoked read touched picture cache")
	}
	// Count only render keys: host resolution uses the same runtime store.
	if dry.calls.Load() != 0 {
		t.Fatal("request used dry store")
	}
	for _, c := range []struct {
		err    error
		status int
	}{{tenancy.ErrPolicyDenied, 403}, {tenancy.ErrPolicyUnavailable, 503}, {contracts.ErrTemporary, 503}, {errors.New("secret provider details"), 500}} {
		source.err = c.err
		w := request("GET", "a.test", path, map[string]string{"If-None-Match": "*"}, true)
		if w.Code != c.status || w.Header().Get("ETag") != "" || strings.Contains(w.Body.String(), "secret provider") {
			t.Fatalf("source refusal %d %s", w.Code, w.Body.String())
		}
	}
	source.err = nil
	source.digest = [32]byte{1}
	if _, err := admin.ExecContext(t.Context(), `UPDATE picture_sources SET readable=true`); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "a.test", path, nil, true); w.Code != 410 {
		t.Fatalf("unknown artifact %d", w.Code)
	}
	source.digest = [32]byte{}
	if w := request("GET", "a.test", path, nil, true); w.Code != 500 {
		t.Fatalf("zero input %d", w.Code)
	}
	source.digest = template.Digest()
	entered, release, revoking := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var paused atomic.Bool
	source.afterRead = func() {
		if paused.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	}
	earlier := make(chan *httptest.ResponseRecorder, 1)
	go func() { earlier <- request("GET", "a.test", path, nil, true) }()
	<-entered
	revoked := make(chan error, 1)
	go func() {
		revoked <- db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			close(revoking)
			return tx.DB().Exec(`UPDATE picture_sources SET readable=false WHERE source_key='shared'`).Error
		})
	}()
	<-revoking
	close(release)
	if w := <-earlier; w.Code != 200 || w.Body.String() != picturetest.Oracle {
		t.Fatalf("earlier consistent decision %d %s", w.Code, w.Body.String())
	}
	if err := <-revoked; err != nil {
		t.Fatal(err)
	}
	source.afterRead = nil
	if w := request("GET", "a.test", path, map[string]string{"If-None-Match": tag}, true); w.Code != 404 {
		t.Fatalf("decision after committed revocation %d", w.Code)
	}
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		result := tx.DB().Exec(`UPDATE picture_sources SET readable=false WHERE source_key='only-b'`)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 0 {
			t.Error("wrong-tenant source write changed a row")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var readable bool
	if err := admin.QueryRowContext(t.Context(), `SELECT readable FROM picture_sources WHERE source_key='only-b'`).Scan(&readable); err != nil || !readable {
		t.Fatalf("other tenant changed: %v %v", readable, err)
	}

	var events int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&events); err != nil || events != 0 {
		t.Fatalf("picture reads emitted domain events: %d %v", events, err)
	}

}
