package auth_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/pkit"
)

// refusal is what New panicked with, or "" when it built the module.
func refusal(deps auth.Deps) (said string) {
	defer func() {
		if r := recover(); r != nil {
			said = fmt.Sprint(r)
		}
	}()
	auth.New(deps)
	return ""
}

// The appended "?token=" needs a path on the tenant's host with nothing after it:
// a relative page, another host, a scheme, a backslash, or a query or fragment of
// its own would each mail a link that does not open the page that was named.
func TestNewRefusesALinkPageThatIsNotAnAbsolutePath(t *testing.T) {
	for _, page := range []string{
		"account/password",
		"https://elsewhere.example/account/password",
		"//elsewhere.example/account/password",
		`/account\password`,
		"/account/password?next=/",
		"/account/password#form",
	} {
		for _, pages := range []auth.LinkPages{{SetPassword: page}, {ConfirmEmail: page}} {
			said := refusal(auth.Deps{LinkPages: pages})
			if !strings.Contains(said, "absolute path") || !strings.Contains(said, fmt.Sprintf("%q", page)) {
				t.Errorf("New(%+v) answered %q, want a refusal naming %q as not an absolute path", pages, said, page)
			}
		}
	}
}

// A mailed link opens a page that reads the token and calls the API; a link
// into the API itself is a JSON door a mail client cannot use. A dot segment
// that resolves there is the same address, and a browser's ".." takes back an
// empty segment rather than the one before it.
func TestNewRefusesALinkPageUnderTheAPIRoot(t *testing.T) {
	for _, page := range []string{
		"/api/v1",
		"/api/v1/auth/password/reset",
		"/api/v1/public/auth/verify-email",
		"/account/../api/v1/public/auth/verify-email",
		"/api/v1//../auth/password/reset",
	} {
		for _, pages := range []auth.LinkPages{{SetPassword: page}, {ConfirmEmail: page}} {
			said := refusal(auth.Deps{LinkPages: pages})
			if !strings.Contains(said, "under the API root") || !strings.Contains(said, fmt.Sprintf("%q", page)) {
				t.Errorf("New(%+v) answered %q, want a refusal naming %q as under the API root", pages, said, page)
			}
		}
	}
}

// Either page may be named alone, the other keeping the workspace screen, and
// an address that only begins with the API root's letters is not under it.
func TestNewTakesAnAbsolutePageOutsideTheAPIRoot(t *testing.T) {
	for _, pages := range []auth.LinkPages{
		{},
		{SetPassword: "/account/password"},
		{ConfirmEmail: "/account/confirm"},
		{SetPassword: "/account/password", ConfirmEmail: "/account/confirm"},
		{SetPassword: "/api/v10/account"},
	} {
		if said := refusal(auth.Deps{LinkPages: pages}); said != "" {
			t.Errorf("New(%+v) refused: %s", pages, said)
		}
	}
}

// A composition that names a page New would refuse is refused by the plan,
// before anything is opened, rather than by a panic in auth's own build.
func TestComposingARefusedLinkPageFailsThePlan(t *testing.T) {
	app := pkit.NewApp("links").Use(auth.MailedLinksOpen(auth.LinkPages{SetPassword: "/api/v1/auth/password/reset"}))
	_, err := app.Plan(pkit.Deployment{Environment: pkit.Development})
	if err == nil || !strings.Contains(err.Error(), "under the API root") {
		t.Fatalf("the plan answered %v, want the page refused as under the API root", err)
	}
}
