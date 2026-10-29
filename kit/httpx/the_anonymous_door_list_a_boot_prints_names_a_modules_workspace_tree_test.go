package httpx_test

// The kernel's honest sentence about which workspace doors admit nobody is
// AnonymousDoors, which kit/app prints at boot; the per-surface count is the
// other reading of the same mount records. A file tree mounted on the workspace
// declares Public(), so it belongs to the first reading as well as the second:
// an operator who reads the count and then looks for the door must find it.
// Nothing here asks whether the file is served to the caller who reaches it —
// which caller a workspace address admits is asked of the answer, elsewhere —
// only that the two readings of one record cannot describe two different doors.

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTheAnonymousDoorListABootPrintsNamesAModulesWorkspaceTree mounts one file
// tree on the workspace beside a sign-in door that declares it answers nobody and
// a guarded one, and reads back what boot prints: the tree and the sign-in door,
// never the guarded route nor a tree of the public face. Every address listed must
// also stand in the mount record on the workspace surface, declared anonymous, so
// the list and the count an operator audits doors from are one decision.
func TestTheAnonymousDoorListABootPrintsNamesAModulesWorkspaceTree(t *testing.T) {
	t.Parallel()
	store := cache.Memory("pkit-anonymous-tree-door")
	t.Cleanup(func() { _ = store.Close() })
	api, _ := httpx.New(httpx.Options{
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
	shell, claimed := api.Surfaces("web").App.Home()
	if !claimed {
		t.Fatal("no claimant took the workspace root, so the shell has no address to stand a tree behind")
	}
	face, claimed := api.Surfaces("web").Public.Home()
	if !claimed {
		t.Fatal("no claimant took the public root, so the face has no tree to keep out of the list")
	}
	httpx.Register(shell, huma.Operation{OperationID: "sign-in", Method: http.MethodPost, Path: "/login"},
		httpx.Public(), ok)
	httpx.Register(shell, huma.Operation{OperationID: "leave", Method: http.MethodPost, Path: "/logout"},
		httpx.Permission("auth:session"), ok)
	const inShell, onFace = "/files", "/assets"
	shell.Static(inShell, fstest.MapFS{"app.css": &fstest.MapFile{Data: []byte(":root{--a:1}")}})
	face.Static(onFace, fstest.MapFS{"site.css": &fstest.MapFile{Data: []byte(":root{--b:2}")}})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the composition describes its doors badly: %v", err)
	}

	want := []string{"GET " + shell.PagePath(inShell) + "/*", "POST " + shell.Path("/login")}
	got := api.AnonymousDoors()
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Errorf("boot prints the workspace doors that admit nobody as %q and asks for %q; the tree asks nothing of "+
			"whoever reaches it, exactly as the sign-in door does, and a list that leaves it out tells the operator the "+
			"workspace admits nobody at a file an anonymous caller reaches", strings.Join(got, " | "), strings.Join(want, " | "))
	}
	for _, door := range got {
		rest := strings.TrimPrefix(strings.TrimPrefix(door, "GET "), "POST ")
		record, found := api.MountedAt(rest)
		if !found {
			t.Errorf("boot prints %q, which no mount record holds; the list and the record are read from one place and "+
				"a door in only one of them is a line an operator cannot check", door)
			continue
		}
		if record.Surface != httpx.SurfaceApp || record.Auth != "public" {
			t.Errorf("boot prints %q among the workspace doors that admit nobody, and the record behind it is on the %s "+
				"surface guarded by %q; the two readings describe one door and must say the same thing about it",
				door, record.Surface, record.Auth)
		}
	}
}
