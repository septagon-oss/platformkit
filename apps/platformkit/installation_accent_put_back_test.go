package main

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/app"
)

// TestTheInstallationsOwnAccentPutBackWritesNothing is the reference composition's
// half of the resolution. app.go names this installation's palette, and neither of
// its two accents clears the site module's legibility rule, so the value a settings
// form was filled with is one no hand could type here: sending it back is the read
// being echoed, not an edit. The save is therefore accepted rather than refused, and
// a second save of the same document writes nothing at all. What the pair rules out
// is a tenant frozen at this palette the day the installation's colours move.
func TestTheInstallationsOwnAccentPutBackWritesNothing(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	before := connectionBody(t, mustBe200(t, cfg, acmeHost))
	accent := design.Default().Light.AccentDefault
	if before["accent"] != accent {
		t.Fatalf("a tenant that named no accent answers %v, want this composition's %v", before["accent"], accent)
	}
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	body := `{"title":"Acme Works","primaryColor":"` + accent + `"}`
	if code, out := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath, body); code != http.StatusOK {
		t.Fatalf("sending the installation's own accent back = %d %s: the value a form was filled with cannot be the value the write refuses", code, out)
	}

	sent := connectionBody(t, mustBe200(t, cfg, acmeHost))
	if sent["accent"] != accent {
		t.Errorf("the face answers %v after the save, want %v", sent["accent"], accent)
	}
	if code, out := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath, body); code != http.StatusOK {
		t.Fatalf("saving the same document again = %d %s", code, out)
	}
	after := connectionBody(t, mustBe200(t, cfg, acmeHost))
	if after["revision"] != sent["revision"] {
		t.Errorf("revision moved from %v to %v: the accent was stored as a colour and the echo read as an edit",
			sent["revision"], after["revision"])
	}
}
