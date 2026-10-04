package main

// A refusal a person meets is worded in the language the request asked for (decision
// 0012), and the pseudo-locale run is what finds a word on it that went around a
// catalogue. The kernel's fault page is the kernel's own copy, so the pt-PT
// catalogues gain its keys: a person asking in Portuguese for an address that is not
// there is not told "Not Found" and "Back to the workspace".

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
	"github.com/septagon-oss/platformkit/ui/legible"
)

func TestTheFaultPageSpeaksTheRequestsLanguage(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	installed := catalogues()
	c := composeCopy(cfg, installed, pseudo.Wrap(installed, nil))
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	for _, fault := range []struct {
		path string
		code int
	}{
		{"/app/no-such-page", http.StatusNotFound},
		{"/app/access-request", http.StatusMethodNotAllowed},
	} {
		code, contentType, body := get(t, cfg, admin, fault.path)
		if code != fault.code || !strings.HasPrefix(contentType, "text/html") {
			t.Fatalf("GET %s answered %d %s, want the %d document", fault.path, code, contentType, fault.code)
		}
		collected, err := legible.Scan(body, pseudo.Wrapped)
		if err != nil {
			t.Fatalf("GET %s: %v", fault.path, err)
		}
		for _, s := range legible.Violations(collected) {
			text := strings.Join(strings.Fields(s.Text), " ")
			if text == http.StatusText(fault.code) || text == "Back to the workspace" {
				t.Errorf("GET %s in pt-PT reads %q at %s, a word no catalogue reached", fault.path, text, s.Path)
			}
		}
	}
}
