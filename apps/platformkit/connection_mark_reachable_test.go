package main

// The half of the mark the document cannot answer from its own row: whether the
// address it would name is an address this installation serves *to the caller it is
// naming it to*.
//
// A logo file defaults to private (modules/file/contracts/file.go), so an
// administrator who uploads a mark and never thinks about visibility has a workspace
// whose face the anonymous caller cannot fetch. A browser drawing that shows a hole
// in a page a person is already reading; a shell drawing it has a broken image on the
// first screen of a journey that begins before sign-in, and no page to fall back to.
// The decision taken here is that the document omits the key — the workspace is
// shown without a mark, which is what a workspace with no mark at all looks like,
// which is what connection_test.go already pins.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
)

func TestTheDocumentNamesOnlyAMarkTheCallerCanFetch(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	const svg = "<svg xmlns='http://www.w3.org/2000/svg'/>"

	// A private mark: stored, named in the settings, and answered to nobody.
	code, body := putFile(t, cfg, admin, filesPath, "logo.svg", "image/svg+xml", svg)
	if code != http.StatusCreated {
		t.Fatalf("uploading the mark = %d %s; the case cannot be asked without a file", code, body)
	}
	private := field(t, body, "id")
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath,
		`{"title":"Acme Works","logoFileId":"`+private+`"}`); code != http.StatusOK {
		t.Fatalf("setting the mark = %d %s", code, body)
	}
	document := connectionBody(t, mustBe200(t, cfg, acmeHost))
	if url, answered := document["logoUrl"]; answered {
		t.Errorf("the document names a private file as the workspace's mark: %v — that address answers %d to the caller this document is answered to",
			url, statusCode(t, cfg, acmeHost, pinnedPublicFile+"/"+private))
	}
	// Omitting the mark costs the face nothing else: the name, the colour and the
	// doors are what a shell needs to draw the screen, and they are all still there.
	if document["name"] != "Acme Works" || document["accent"] != "#2563eb" {
		t.Errorf("a workspace with an unreachable mark answers %v, want the face minus the mark alone", document)
	}

	// A public mark: the same key, the same builder, and an address the very same
	// anonymous caller follows to the bytes.
	code, body = putFile(t, cfg, admin, filesPath+"?visibility=public", "mark.svg", "image/svg+xml", svg)
	if code != http.StatusCreated {
		t.Fatalf("uploading the public mark = %d %s", code, body)
	}
	public := field(t, body, "id")
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath,
		`{"title":"Acme Works","logoFileId":"`+public+`"}`); code != http.StatusOK {
		t.Fatalf("setting the public mark = %d %s", code, body)
	}
	document = connectionBody(t, mustBe200(t, cfg, acmeHost))
	url, _ := document["logoUrl"].(string)
	if want := pinnedPublicFile + "/" + public; url != want {
		t.Fatalf("the document omits or misnames a mark its caller can fetch: %q, want %q — a workspace shown without a mark it has is the same broken first screen from the other side", url, want)
	}
	if code, got := do(t, cfg, nil, http.MethodGet, acmeHost, url, ""); code != http.StatusOK || !strings.Contains(got, "<svg") {
		t.Errorf("GET %s with no session = %d %s", url, code, got)
	}

	// A mark whose file is gone: the id carries no foreign key by decision, so the
	// row outlives the bytes, and the answer is the same omission rather than a
	// document that names an address nothing serves.
	if code, body := do(t, cfg, admin, http.MethodDelete, acmeHost, filesPath+"/"+public, ""); code != http.StatusNoContent {
		t.Fatalf("deleting the mark = %d %s; the case cannot ask about a gone file", code, body)
	}
	document = connectionBody(t, mustBe200(t, cfg, acmeHost))
	if _, answered := document["logoUrl"]; answered {
		t.Errorf("the document names a mark whose file is gone: %v", document["logoUrl"])
	}
	if document["name"] != "Acme Works" {
		t.Errorf("a gone mark took the workspace's name with it: %v", document["name"])
	}
}

// statusCode asks the running server what one anonymous GET gets, for a failure
// message that has to say what the caller would actually meet.
func statusCode(t *testing.T, cfg config.Config, host, path string) int {
	t.Helper()
	code, _ := do(t, cfg, nil, http.MethodGet, host, path, "")
	return code
}
