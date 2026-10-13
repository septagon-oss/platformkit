package main

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/app"
)

func TestOmittingTheColourReturnsToThePaletteOnce(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath,
		`{"title":"Acme Works","theme":"dark","primaryColor":"#b45309"}`); code != http.StatusOK {
		t.Fatalf("choosing the colour = %d %s", code, body)
	}
	chosen := connectionBody(t, mustBe200(t, cfg, acmeHost))
	if chosen["accent"] != "#b45309" || chosen["theme"] != "dark" {
		t.Fatalf("chosen face = %v, want the chosen colour on dark", chosen)
	}

	const omitted = `{"title":"Acme Works","theme":"dark"}`
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath, omitted); code != http.StatusOK {
		t.Fatalf("omitting the colour = %d %s, want 200", code, body)
	}
	returned := connectionBody(t, mustBe200(t, cfg, acmeHost))
	want := design.Default().Dark.AccentDefault
	if returned["accent"] != want || returned["theme"] != "dark" {
		t.Errorf("returned face = %v, want the palette's dark accent %s", returned, want)
	}
	if returned["revision"] != chosen["revision"].(float64)+1 {
		t.Errorf("returning to the palette moved revision %v to %v, want one write", chosen["revision"], returned["revision"])
	}

	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath, omitted); code != http.StatusOK {
		t.Fatalf("repeating the omission = %d %s, want 200", code, body)
	}
	after := connectionBody(t, mustBe200(t, cfg, acmeHost))
	if after["accent"] != want || after["revision"] != returned["revision"] {
		t.Errorf("repeated omission changed the face from %v to %v", returned, after)
	}
}
