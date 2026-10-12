package main

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
)

// TestTheEmptyColourIsRefusedAtTheWireAndMovesNothing holds the write door's own
// account of the empty colour: it is a stored value and never a wire value. The
// published pattern admits no empty string, so a PUT that sends one is refused
// before the service sees it, and the refusal writes nothing — the colour the
// tenant chose is still the face's answer and the revision has not moved.
func TestTheEmptyColourIsRefusedAtTheWireAndMovesNothing(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	if code, out := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath, `{"title":"Acme Works","primaryColor":"#b45309"}`); code != http.StatusOK {
		t.Fatalf("choosing a colour = %d %s", code, out)
	}
	chosen := connectionBody(t, mustBe200(t, cfg, acmeHost))
	if chosen["accent"] != "#b45309" {
		t.Fatalf("the face answers %v after the choice, want #b45309", chosen["accent"])
	}

	code, out := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath, `{"title":"Acme Works","primaryColor":""}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("sending the empty colour = %d %s, want 422: the wire's own pattern admits no empty string", code, out)
	}
	after := connectionBody(t, mustBe200(t, cfg, acmeHost))
	if after["accent"] != chosen["accent"] || after["revision"] != chosen["revision"] {
		t.Errorf("a refused write moved the face from %v at revision %v to %v at revision %v",
			chosen["accent"], chosen["revision"], after["accent"], after["revision"])
	}
}
