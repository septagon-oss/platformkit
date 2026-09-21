package httpx_test

// A file tree is mounted inside the chain of the surface it was given, and the
// claim has to cover the tree's misses as well as its hits: the person who asked
// for a stylesheet this deployment stopped shipping is refused by the host they
// were talking to, in the shape every other refusal of that host takes, with that
// surface's headers on it. A tree that answers its own 404 is a hole in the promise
// the mount table lists the tree for — and a directory listing, which is what
// net/http offers instead, is an answer nobody mounted.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAMountedTreeRefusesWhatItDoesNotHold(t *testing.T) {
	s := newSurfaces(t)
	tree := fstest.MapFS{
		"app.css":      &fstest.MapFile{Data: []byte(":root{--a:1}")},
		"js/app.js":    &fstest.MapFile{Data: []byte("void 0")},
		"fonts/x.woff": &fstest.MapFile{Data: []byte("woff")},
	}
	s.api.Surfaces("admin").App.Static("/assets", tree)
	if err := s.api.ValidateDeclarations(); err != nil {
		t.Fatalf("the tree does not describe itself: %v", err)
	}

	ask := func(path, accept string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "http://"+host+"/app/admin/assets"+path, nil)
		r.Header.Set("Accept", accept)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, r)
		return w
	}

	// The tree, working: every file it holds is served, with the workspace's own
	// answer about caching. What follows is about the addresses it does not hold.
	for _, at := range []string{"/app.css", "/js/app.js", "/fonts/x.woff"} {
		got := ask(at, "text/css")
		if got.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %q; this case is about a tree that serves its own files", at, got.Code, got.Body.String())
		}
	}

	// The refusals, which are the host's and not the file server's: the same status,
	// the same body and the same media type an address nobody mounted gives, and
	// dressed by the surface the tree stands in — which is what says the answer came
	// through the chain rather than beside it.
	const unmounted = "nothing-is-mounted-here"
	want := ask("/"+unmounted, "application/json")
	if want.Code != http.StatusNotFound {
		t.Fatalf("an address nobody mounted in the tree's namespace = %d; this case compares against that answer", want.Code)
	}
	for _, at := range []string{"/absent.css", "/js/absent.js", "/", "", "/js", "/js/", "/fonts"} {
		got := ask(at, "application/json")
		if got.Code != want.Code || got.Header().Get("Content-Type") != want.Header().Get("Content-Type") ||
			!strings.Contains(got.Body.String(), "nothing is served at this address") {
			t.Errorf("GET the tree at %q answered %d %s where the same namespace answers %d %s for an address "+
				"nobody mounted; a file the tree does not hold is the host's refusal and not net/http's note",
				at, got.Code, got.Header().Get("Content-Type"), want.Code, want.Header().Get("Content-Type"))
		}
		if got.Header().Get("Cache-Control") != "no-store" || !strings.Contains(got.Header().Get("X-Robots-Tag"), "noindex") {
			t.Errorf("the tree's refusal at %q is dressed %q %q, which is not the workspace surface dressing an answer",
				at, got.Header().Get("Cache-Control"), got.Header().Get("X-Robots-Tag"))
		}
		if strings.Contains(got.Header().Get("Content-Type"), "text/html") || strings.Contains(got.Body.String(), "<ul>") {
			t.Errorf("GET the tree at %q answered a directory out of the filesystem: %s", at, got.Body.String())
		}
	}

	// And the same refusals asked as a browser navigation: whatever shape this
	// composition's refusal takes, it is not the plain-text note this package says no
	// browser is shown any more.
	if page := ask("/absent.css", "text/html"); strings.Contains(page.Body.String(), "404 page not found") ||
		strings.Contains(page.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("a browser navigating to a missing file of the tree is shown %q (%s)",
			strings.TrimSpace(page.Body.String()), page.Header().Get("Content-Type"))
	}

	// The tree's own address space is not a list of what the shell ships.
	if listing := ask("", "text/html"); listing.Code == http.StatusOK {
		t.Errorf("the mount prefix answers 200 and names what the tree holds: %s", listing.Body.String())
	}
}
