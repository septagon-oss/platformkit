package httpx_test

// A module's file tree reaches its operator through the mount record and through
// nothing else: it is no operation, so the OpenAPI document and the list of
// grants a request must carry never saw it, while the per-surface counts kit/app
// prints at boot do count it. Were a tree ever registered as an operation, the
// published document would name a door that declares no grant; were it recorded
// twice, the boot line would count a door nobody opens, which is the number an
// operator audits a site's anonymous files from.

import (
	"context"
	"fmt"
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

// TestAModulesFileTreeIsADoorTheAuditListsAndTheOperationDocumentDoesNot composes
// one module twice — once with a file tree behind each of the two page doors a
// module may stand one behind, the public face and the workspace, and once with
// neither — and asks that the trees add one recorded door per surface and nothing
// at all to the operation document or the required grants. Each tree must then
// stand as one record: by GET, as a document, on the surface it was mounted on,
// under the module that was handed the root, at the address that surface's own
// page writes for it. The public tree's file is fetched at that address first, so
// the record is compared with an address that answers rather than with a spelling;
// the workspace tree is read at its record alone, because which caller its address
// admits is the workspace's own question, not the record's.
func TestAModulesFileTreeIsADoorTheAuditListsAndTheOperationDocumentDoesNot(t *testing.T) {
	t.Parallel()

	const (
		frontTree = "/web/assets" // what the site's shell writes for its own tree
		shellTree = "/operator"   // a tree stood inside the workspace instead
		treeFile  = "app.css"
		treeBody  = ":root{--a:1}"
	)

	// compose builds one composition of one module named "web", standing a tree
	// behind each of its two page doors when trees is set, and answers what the
	// composition says about itself: the doors it mounted, how many per surface
	// the boot line counts them, the operations its document carries, the grants a
	// request must carry, the two addresses its chrome writes for the trees, and
	// the handler. One function rather than two fixtures, so the two readings can
	// differ only in the trees.
	compose := func(t *testing.T, trees bool) (
		door []httpx.MountedRoute, counts map[httpx.Surface]int, handled int,
		required, atFront, atShell string, serve http.Handler) {
		t.Helper()
		name := "pkit-tree-audit-none"
		if trees {
			name = "pkit-tree-audit-two"
		}
		store := cache.Memory(name)
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
		front, claimed := api.Surfaces("web").Public.Home()
		if !claimed {
			t.Fatal("no claimant took the public root, so no face has a tree to be recorded")
		}
		shell, claimed := api.Surfaces("web").App.Home()
		if !claimed {
			t.Fatal("no claimant took the workspace root, so no shell has a tree to be recorded")
		}
		if trees {
			front.Static(frontTree, fstest.MapFS{treeFile: &fstest.MapFile{Data: []byte(treeBody)}})
			shell.Static(shellTree, fstest.MapFS{treeFile: &fstest.MapFile{Data: []byte(":root{--b:2}")}})
		}
		if err := api.ValidateDeclarations(); err != nil {
			t.Fatalf("the composition describes its trees badly: %v", err)
		}
		return api.Mounted(), api.MountedBySurface(), len(api.Recorded()), fmt.Sprint(api.Required()),
			front.PagePath(frontTree), shell.PagePath(shellTree), router
	}

	door, counts, handled, required, atFront, atShell, serve := compose(t, true)
	_, plainCounts, plainHandled, plainRequired, plainFront, plainShell, _ := compose(t, false)

	// The address a surface writes for a tree is its own, not a consequence of
	// what stands beneath it: mounting must not move what the chrome links.
	if atFront != plainFront || atShell != plainShell {
		t.Errorf("the pages name their trees at %q and %q mounted and at %q and %q unmounted; the address a shell links is "+
			"one decision, and a tree that moved it would break every page that already links it",
			atFront, atShell, plainFront, plainShell)
	}
	// A tree is no operation and asks nothing of whoever reaches it, so neither the
	// document nor the grant list can see it.
	if handled != plainHandled {
		t.Errorf("the composition carries %d operations beside its two file trees and %d without them; a tree is mounted, "+
			"never declared, so the published document names the same doors either way", handled, plainHandled)
	}
	if required != plainRequired {
		t.Errorf("the composition requires %s beside its trees and %s without them; a stylesheet asks no grant, and a tree "+
			"that added one would ask the grant gate to deny a file the browser navigates to", required, plainRequired)
	}
	// The boot line's own reading: one door per tree, and nothing on the control
	// plane, where no tree may stand at all.
	for _, want := range []struct {
		surface httpx.Surface
		added   int
	}{{httpx.SurfacePublic, 1}, {httpx.SurfaceApp, 1}, {httpx.SurfaceOps, 0}} {
		if got := counts[want.surface] - plainCounts[want.surface]; got != want.added {
			t.Errorf("the boot line counts %d more door(s) on the %s surface once two file trees are mounted and asks for %d; "+
				"an operator audits the doors that admit an anonymous caller from that count, so a tree recorded twice or not "+
				"at all is a line that misreads", got, want.surface, want.added)
		}
	}

	w := httptest.NewRecorder()
	serve.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://"+host+atFront+"/"+treeFile, nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), treeBody) {
		t.Fatalf("the public tree's file answers at %q with %d %q; this case compares the record of a mount with an address "+
			"that serves, so it needs the answer first", atFront+"/"+treeFile, w.Code, w.Body.String())
	}
	for _, want := range []struct {
		surface httpx.Surface
		at      string
	}{{httpx.SurfacePublic, atFront}, {httpx.SurfaceApp, atShell}} {
		recorded, doors := "", 0
		for _, m := range door {
			if m.Surface != want.surface || !strings.HasPrefix(m.Path, want.at) {
				continue
			}
			recorded, doors = m.Path, doors+1
			if m.Method != http.MethodGet || !m.Page {
				t.Errorf("the audit lists the tree at %q as %s with page=%v; a file tree is a document a browser navigates to "+
					"with GET, and the boot line counts those apart from the values a command answers", want.at, m.Method, m.Page)
			}
			if m.Module != "web" {
				t.Errorf("the audit lists the tree at %q under module %q, not under the claimant that was handed the root",
					want.at, m.Module)
			}
		}
		if doors != 1 {
			t.Errorf("the audit lists %d door(s) beneath %q on the %s surface (last at %q); one tree mounted once is one door "+
				"for an operator to audit", doors, want.at, want.surface, recorded)
			continue
		}
		if recorded != want.at+"/*" {
			t.Errorf("the audit lists the tree whose page names it at %q at %q; the record and the served address are one "+
				"decision, and the audit of who may reach a site's files reads the first of them", want.at, recorded)
		}
	}
}
