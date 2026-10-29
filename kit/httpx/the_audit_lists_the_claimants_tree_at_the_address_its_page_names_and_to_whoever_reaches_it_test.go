package httpx_test

// A composition tells its operator which doors admit an anonymous caller by reading
// its own mount record, and the boot line prints that record; a file tree is in it,
// declared anonymous, because a stylesheet asks nothing of whoever asks for it. The
// sibling cases ask the answering half of the claimant's tree — the bytes come back
// where its page says they come back, the gate keeps the root itself — and none asks
// the record beside it. Were the record ever to name a different address from the
// mount, or to call an anonymous door a signed-in one, every page of the site would
// still draw in a browser that already has a session and the audit would describe a
// door nobody serves.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTheAuditListsTheClaimantsTreeAtTheAddressItsPageNamesAndToWhoeverReachesIt
// mounts a file tree the way the site's shell does, inside the root its module
// claimed and under its own name, and reads the composition's record of that mount:
// one door, GET, a document, on the public surface, at the address PagePath writes
// for the same relative path, open to whoever reaches it. The file answers there, so
// the record is compared with a live answer and not with a spelling, and no door of
// the record stands at the claimed root's own file namespace.
func TestTheAuditListsTheClaimantsTreeAtTheAddressItsPageNamesAndToWhoeverReachesIt(t *testing.T) {
	t.Parallel()
	store := cache.Memory("pkit-claimant-audit-door")
	t.Cleanup(func() { _ = store.Close() })
	api, router := httpx.New(httpx.Options{
		Unwired: true, Cache: store, PublicHost: host, Installation: installationHost,
		Log: slog.New(slog.DiscardHandler),
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], _ string) (tenancy.Tenant, error) {
			return tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return true, nil }),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
	})
	home, claimed := api.Surfaces("web").Public.Home()
	if !claimed {
		t.Fatal("no claimant took the public root, so no shell has a tree recorded inside it")
	}
	const mounted = "/web/assets"
	home.Static(mounted, fstest.MapFS{"app.css": &fstest.MapFile{Data: []byte(":root{--a:1}")}})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the claimant's tree describes itself badly: %v", err)
	}

	at := home.PagePath(mounted)
	asked := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://"+host+path, nil))
		return w
	}
	if served := asked(at + "/app.css"); served.Code != http.StatusOK || !strings.Contains(served.Body.String(), "--a:1") {
		t.Fatalf("the file of the claimant's tree at %q answers %d %q; this case compares the composition's record of "+
			"that mount with a live answer, so it needs the answer first", at+"/app.css", served.Code, served.Body.String())
	}

	door := ""
	doors := 0
	for _, m := range api.Mounted() {
		if m.Surface != httpx.SurfacePublic || !strings.HasPrefix(m.Path, at) {
			continue
		}
		doors++
		door = m.Path
		if m.Method != http.MethodGet || !m.Page {
			t.Errorf("the audit lists the claimant's tree as %s with page=%v; a file tree is a document the browser "+
				"navigates to with GET, and the boot line counts those apart from the values a command answers", m.Method, m.Page)
		}
		if m.Module != "web" {
			t.Errorf("the audit lists the claimant's tree under module %q, not under the claimant that was handed the root", m.Module)
		}
		if m.Auth != "public" {
			t.Errorf("the audit lists the claimant's tree as %q, which is not the anonymous door a stylesheet is: the "+
				"boot line would tell the operator this door remembers whoever reaches it, and an anonymous visitor of the "+
				"public face would be refused the site's own stylesheet", m.Auth)
		}
	}
	if doors != 1 {
		t.Errorf("the audit lists %d doors at %q (last at %q); a shell keeps one tree under one name, and two records "+
			"of it are two doors an operator would have to audit", doors, at, door)
	}
	if door != at+"/*" {
		t.Errorf("the audit lists the claimant's tree at %q while its page names it at %q and the mount answers there: "+
			"the record and the served address are one decision, and the audit of who may reach the site's files reads "+
			"the first of them", door, at)
	}

	for _, m := range api.Mounted() {
		if m.Surface == httpx.SurfacePublic && (m.Path == "/*" || m.Path == "/*.") {
			t.Errorf("the audit lists a file tree at %q, the claimed root's own file namespace, where the routes of the "+
				"site answer: chi would let that tree answer for everything beneath it", m.Path)
		}
	}
}
