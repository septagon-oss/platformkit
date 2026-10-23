package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestALinkCannotClaimThatASessionEnded: the sign-in line "Sign in again" follows the
// session cookie the request carries, never a query parameter a link can set.
func TestALinkCannotClaimThatASessionEnded(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	for _, q := range []string{"?again=1", "?expired=1", "?session=ended"} {
		code, _, body := browse(t, cfg, "/app/admin/login"+q, nil)
		if code != http.StatusOK {
			t.Fatalf("GET /app/admin/login%s = %d", q, code)
		}
		if strings.Contains(body, signInAgain) || !strings.Contains(body, signInStranger) {
			t.Errorf("a link %s made the sign-in page claim an ended session: %s", q, trimHTML(body))
		}
	}
}
