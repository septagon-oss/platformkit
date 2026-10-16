package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheContentListDrawsItsCoverAsAPicture asks the reference composition's own
// workspace list what it shows for a page with a cover. The catalogue tells a
// phone to fetch the cover from the file module's door; the generated web list
// in the same composition must draw the same picture from the same door, and
// never print the file id as the cell's words.
func TestTheContentListDrawsItsCoverAsAPicture(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	code, body := putFile(t, cfg, who, filesPath, "cover.svg", "image/svg+xml", "<svg xmlns='http://www.w3.org/2000/svg'/>")
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", filesPath, code, body)
	}
	cover := field(t, body, "id")
	const title = "A page with a cover"
	input, _ := json.Marshal(map[string]any{"slug": "page-with-a-cover", "title": title, "kind": "page", "coverImageId": cover})
	if code, body := do(t, cfg, who, http.MethodPost, acmeHost, contentPath, string(input)); code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}

	code, page := do(t, cfg, who, http.MethodGet, acmeHost, "/app/content/contents", "")
	if code != http.StatusOK || !strings.Contains(page, title) {
		t.Fatalf("GET /app/content/contents = %d, want the list naming %q", code, title)
	}
	door := strings.Replace(filecontracts.PictureSource, "{id}", cover, 1)
	if !strings.Contains(page, `src="`+door) {
		t.Errorf("the content list draws no picture from %s for the page's cover", door)
	}
	if strings.Contains(page, ">"+cover+"<") {
		t.Errorf("the content list prints the cover's file id %s as a cell's text", cover)
	}
}
