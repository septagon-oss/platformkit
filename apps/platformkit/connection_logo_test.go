package main

// The workspace's mark, followed through the connection document the way
// review_surfaces_test.go follows it through the public page: uploaded, named in
// the site settings, answered to a caller with no session, and fetchable at the
// address the document gives. connection_test.go pins only the omission half —
// a workspace with no mark answers no logoUrl — and this is the half where a
// shell actually draws one, which is the brief's own headline ("name, logo and
// colour before sign-in").

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
)

func TestTheWorkspacesMarkIsReachableBeforeSignIn(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	const svg = "<svg xmlns='http://www.w3.org/2000/svg'/>"
	code, body := putFile(t, cfg, admin, filesPath+"?visibility=public", "logo.svg", "image/svg+xml", svg)
	if code != http.StatusCreated {
		t.Fatalf("uploading the logo = %d %s; the case cannot be asked without a file", code, body)
	}
	logo := field(t, body, "id")
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath,
		`{"title":"Acme Works","logoFileId":"`+logo+`"}`); code != http.StatusOK {
		t.Fatalf("setting the logo = %d %s", code, body)
	}

	// The document, with no session: the mark is named, at the public door's own
	// address, and that address answers the bytes to the same anonymous caller —
	// and the answer plants no cookie, because reading a workspace's face is not
	// the start of a session.
	if got := responseHeader(t, cfg, nil, acmeHost, connectionPath, "Set-Cookie"); got != "" {
		t.Errorf("an anonymous read of the face set a cookie: %q", got)
	}
	document := connectionBody(t, mustBe200(t, cfg, acmeHost))
	url, _ := document["logoUrl"].(string)
	if want := pinnedPublicFile + "/" + logo; url != want {
		t.Fatalf("the document names the mark as %q, want %q: a device is told an address, so it has to be one this installation serves", url, want)
	}
	if code, got := do(t, cfg, nil, http.MethodGet, acmeHost, url, ""); code != http.StatusOK || !strings.Contains(got, "<svg") {
		t.Errorf("GET %s with no session = %d %s; a mark the document names is drawn before sign-in or it is not a mark", url, code, got)
	}

	// A mark whose file is private: whether the document should name it at all is
	// an open question (today it does, and an anonymous caller who follows it gets
	// a 404 — the web page's <img> has the same shape). This pin holds whichever
	// way that is decided: the document still answers, the address it names — if
	// it names one — is the public door's, and that door never hands over the
	// bytes of a file that is not public.
	code, body = putFile(t, cfg, admin, filesPath, "secret.svg", "image/svg+xml", svg)
	if code != http.StatusCreated {
		t.Fatalf("uploading the private file = %d %s", code, body)
	}
	hidden := field(t, body, "id")
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath,
		`{"title":"Acme Works","logoFileId":"`+hidden+`"}`); code != http.StatusOK {
		t.Fatalf("setting the private logo = %d %s", code, body)
	}
	document = connectionBody(t, mustBe200(t, cfg, acmeHost))
	if url, _ := document["logoUrl"].(string); url != "" && url != pinnedPublicFile+"/"+hidden {
		t.Errorf("the document names the private mark as %q, which is neither omitted nor the public door's address", url)
	}
	if code, got := do(t, cfg, nil, http.MethodGet, acmeHost, pinnedPublicFile+"/"+hidden, ""); code == http.StatusOK {
		t.Errorf("the public door served a private file's bytes: %s", got)
	}
}
