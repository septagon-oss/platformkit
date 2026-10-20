package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"log/slog"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The declared protocol address: a module names the address its protocol names,
// and the kernel serves its router's chain there. These cases ask the three
// questions that decide whether that is a door or a hole — where it answers,
// which chain answers there, and what a composition has to say to be asked in a
// protocol's own media type.

// protocolBody is a response body that names the media type it is always served
// as. The check huma makes is on the body field's type, not the response
// struct's, which is why this is its own type.
type protocolBody struct {
	Name string `json:"name"`
}

func (protocolBody) ContentType(string) string { return "application/scim+json; charset=utf-8" }

type protocolProbe struct {
	Body protocolBody
}

// protocolAPI builds the fixture's API with a protocol's media type registered
// as JSON, which is the composition's half of serving a protocol: a module that
// speaks application/scim+json cannot reach the API's format table, and a body
// type nobody registered is refused before any route runs.
func protocolAPI(t *testing.T, jsonMediaTypes ...string) (*httpx.API, http.Handler, *fixture) {
	t.Helper()
	admin, app := dbtest.Schema(t)
	f := &fixture{
		tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"},
		app:    app,
		logs:   &lines{},
	}
	f.exec = func(query string) {
		t.Helper()
		if _, err := admin.ExecContext(t.Context(), query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	api, router := httpx.New(httpx.Options{
		Cache:          cache.Memory("pkit"),
		Installation:   host,
		PublicHost:     host,
		Tenants:        f,
		Conn:           app,
		Authorize:      f,
		Entitle:        f,
		Authenticate:   f.authenticate,
		JSONMediaTypes: jsonMediaTypes,
		Log:            slog.New(slog.NewTextHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	return api, router, f
}

// TestARouteAtADeclaredAddressAnswersAtThatAddressWithItsOwnChain is the whole
// claim in one request. The door stands where the module said it stands — which
// the fixed prefix table would have called the public face, the surface that
// resolves no caller — and it is still the workspace door: its headers, its
// refusal of an anonymous caller, its entry in the mount table.
func TestARouteAtADeclaredAddressAnswersAtThatAddressWithItsOwnChain(t *testing.T) {
	api, router, f := setup(t)
	f.allow = true
	f.signedIn()

	probeApp := api.Surfaces(probe).App.At("/scim/v2")
	httpx.Register(probeApp, huma.Operation{
		OperationID: "protocol-probe", Method: http.MethodGet, Path: "/things",
	}, httpx.Permission("task:read"), func(context.Context, *struct{}) (*protocolProbe, error) {
		return &protocolProbe{Body: protocolBody{Name: "thing"}}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("a route mounted at a declared address does not declare itself: %v", err)
	}

	// The table's own answer about /scim/v2/things is public: it names no
	// prefix. A chain that answered Public here would parse no session, and the
	// door this module just mounted would answer nobody at all.
	got := get(t, router, "/scim/v2/things")
	if got.Code != http.StatusOK {
		t.Fatalf("GET /scim/v2/things = %d %s, want 200 — the module declared this address, so the door is here",
			got.Code, got.Body.String())
	}
	if h := got.Header().Get("Cache-Control"); h != "no-store" {
		t.Errorf("the declared address answered Cache-Control %q, want no-store: the workspace's headers are the answer, and a protocol door whose responses a replica may keep is the bug this case exists for", h)
	}

	var surface httpx.Surface
	var page bool
	for _, route := range api.Mounted() {
		if route.Path == "/scim/v2/things" {
			surface, page = route.Surface, route.Page
		}
	}
	if surface != httpx.SurfaceApp {
		t.Errorf("the mount table records the declared address on %s, want app: the declaration moved where the router stands, never what it carries", surface)
	}
	if page {
		t.Error("the mount table records a JSON route as a page")
	}

	// An anonymous caller is refused the way the workspace refuses everybody. A
	// protocol's address buys no way in.
	f.allow = false
	f.signedIn()
	denied := get(t, router, "/scim/v2/things")
	if denied.Code == http.StatusOK {
		t.Fatalf("an anonymous request to the declared address was served: %s", denied.Body.String())
	}

	// A document, not a page: the module's own pages keep the surface's page
	// prefix whatever else it declared, which is what its links and its cache
	// headers are written against. Asked of the router the route was mounted on,
	// which is the one holding the declaration: `At` after `ValidateDeclarations`
	// is refused rather than honoured, so a fresh router would answer with the
	// table's prefix and prove nothing about the door that actually opened.
	if at := probeApp.Path("/things"); at != "/scim/v2/things" {
		t.Errorf("Path at a declared address = %q, want /scim/v2/things", at)
	}
	if p := probeApp.PagePath("settings"); p != "/app/probe/settings" {
		t.Errorf("the module's page lives at %q, want the workspace's own page prefix", p)
	}
}

// TestADeclaredAddressIsRefusedAtMount walks the shapes that cannot be declared.
// Each one stops the process from listening rather than being corrected quietly:
// a client is configured with one address by hand, and an installation that
// quietly moved it is an incident with a long fuse.
func TestADeclaredAddressIsRefusedAtMount(t *testing.T) {
	for _, tc := range []struct{ name, at, wants string }{
		{"relative", "scim/v2", "absolute"},
		{"the table's own", "/api/v1/scim", "surface table"},
		{"a prefix of the table", "/api", "is or is beneath"},
		{"a pattern", "/scim/%d", "a declared address is an absolute"},
		{"upper case", "/SCIM/v2", "a declared address is an absolute"},
		{"a trailing slash", "/scim/v2/", "a declared address is an absolute"},
		{"an empty address", "", "a declared address is an absolute"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, _, _ := setup(t)
			httpx.Register(api.Surfaces(probe).App.At(tc.at), huma.Operation{
				OperationID: "refuse-" + strings.ReplaceAll(tc.name, " ", "-"), Method: http.MethodGet, Path: "/x",
			}, httpx.Public(), func(context.Context, *struct{}) (*body, error) { return &body{}, nil })
			err := api.ValidateDeclarations()
			if err == nil || !strings.Contains(err.Error(), tc.wants) {
				t.Fatalf("declaring %q was refused with %v, which does not say %q", tc.at, err, tc.wants)
			}
		})
	}

	// One address, one owner. A second module that declares the same door, or
	// the half of it, is refused, and the message names the first of them so the
	// second knows who to ask.
	t.Run("claimed twice", func(t *testing.T) {
		api, _, _ := setup(t)
		httpx.Register(api.Surfaces(probe).App.At("/scim/v2"), huma.Operation{
			OperationID: "first-claim", Method: http.MethodGet, Path: "/x",
		}, httpx.Public(), func(context.Context, *struct{}) (*body, error) { return &body{}, nil })
		httpx.Register(api.Surfaces("other").App.At("/scim/v2"), huma.Operation{
			OperationID: "second-claim", Method: http.MethodGet, Path: "/y",
		}, httpx.Public(), func(context.Context, *struct{}) (*body, error) { return &body{}, nil })
		err := api.ValidateDeclarations()
		if err == nil || !strings.Contains(err.Error(), "one owner") || !strings.Contains(err.Error(), "probe") {
			t.Fatalf("two modules declaring /scim/v2 were refused with %v, which does not name both of them", err)
		}
	})

	// The same module asking twice for the same address is one declaration, not
	// a collision: the build that answers questions about its own routes may
	// register them more than once.
	t.Run("the same module twice", func(t *testing.T) {
		api, _, _ := setup(t)
		for _, id := range []string{"asked-once", "asked-twice"} {
			httpx.Register(api.Surfaces(probe).App.At("/scim/v2"), huma.Operation{
				OperationID: id, Method: http.MethodGet, Path: "/" + id,
			}, httpx.Public(), func(context.Context, *struct{}) (*body, error) { return &body{}, nil })
		}
		if err := api.ValidateDeclarations(); err != nil {
			t.Fatalf("one module declaring one address twice was refused: %v", err)
		}
	})

	// The declared address is a prefix in the same sense /api/v1 is: a route
	// that reaches it by writing it out relative to some other router would
	// answer at two addresses, and the doubling nobody notices is the one that
	// ships.
	t.Run("a route that names the declared address", func(t *testing.T) {
		api, _, _ := setup(t)
		httpx.Register(api.Surfaces(probe).App.At("/scim/v2"), huma.Operation{
			OperationID: "the-claim", Method: http.MethodGet, Path: "/things",
		}, httpx.Public(), func(context.Context, *struct{}) (*body, error) { return &body{}, nil })
		httpx.Register(api.Surfaces(probe).App, huma.Operation{
			OperationID: "the-doubler", Method: http.MethodGet, Path: "/scim/v2/things",
		}, httpx.Public(), func(context.Context, *struct{}) (*body, error) { return &body{}, nil })
		err := api.ValidateDeclarations()
		if err == nil || !strings.Contains(err.Error(), "names a prefix") {
			t.Fatalf("a route writing the declared address into a relative path was refused with %v, which does not say it names a prefix", err)
		}
	})

	// A credential is still a credential. The declaration moves an address and
	// gives nobody a way in: a credentialed route mounted at a declared address
	// on the public face is the refusal the public router already was.
	t.Run("a credentialed door on the public face", func(t *testing.T) {
		api, _, _ := setup(t)
		httpx.Register(api.Surfaces(probe).Public.At("/vendor/v1"), huma.Operation{
			OperationID: "public-credentialed", Method: http.MethodGet, Path: "/things",
		}, httpx.Permission("task:read"), func(context.Context, *struct{}) (*body, error) { return &body{}, nil })
		err := api.ValidateDeclarations()
		if err == nil || !strings.Contains(err.Error(), "public surface resolves no session") {
			t.Fatalf("a Permission route at a declared public address was refused with %v, which is not the public router's own sentence", err)
		}
	})
}

// TestARequestSpeakingAnotherJSONMediaTypeIsServed, and the answer it names in
// the response. A connector sends the type its protocol names; before the
// composition could say that type is JSON, huma answered it with 415 — the door
// existed and refused every client that spoke it.
func TestARequestSpeakingAnotherJSONMediaTypeIsServed(t *testing.T) {
	api, router, f := protocolAPI(t, "application/scim+json")
	f.allow = true
	f.signedIn()

	httpx.Register(api.Surfaces(probe).App.At("/scim/v2"), huma.Operation{
		OperationID: "protocol-echo", Method: http.MethodPost, Path: "/things",
	}, httpx.Permission("task:read"), func(_ context.Context, in *struct {
		Body struct {
			Name string `json:"name"`
		}
	}) (*protocolProbe, error) {
		return &protocolProbe{Body: protocolBody{Name: in.Body.Name}}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted route does not declare itself: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, "/scim/v2/things", strings.NewReader(`{"name":"ada"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/scim+json")
	req.Host = host
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST with Content-Type application/scim+json = %d %s, want 200 — the composition said this type is JSON",
			w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"ada"`) {
		t.Errorf("the body was not read as JSON: %s", w.Body.String())
	}
	// The answer names the protocol's type even though the client asked for
	// nothing in particular: the body says what it is, and huma honours it.
	if ct := w.Header().Get("Content-Type"); ct != "application/scim+json; charset=utf-8" {
		t.Errorf("the answer came back as %q, want the media type the body names", ct)
	}
}

// TestAMediaTypeThatIsNotAMediaTypeRefusesTheBuild: an entry of the wrong shape
// registers nothing at all, so the protocol's own clients would meet a 415
// nobody can explain. The composition is refused instead of half-built.
func TestAMediaTypeThatIsNotAMediaTypeRefusesTheBuild(t *testing.T) {
	for _, mt := range []string{"scim", "application/", "application/scim json", "APPLICATION/SCIM+JSON", ""} {
		api, _, _ := protocolAPI(t, mt)
		err := api.ValidateDeclarations()
		if err == nil || !strings.Contains(err.Error(), "JSONMediaTypes") {
			t.Errorf("a composition naming %q as a JSON media type was refused with %v, which does not name the option", mt, err)
		}
	}
}
