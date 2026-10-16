package internal_test

// What a deployment that set no auth.factor_key actually answers at the doors
// of the second factor — and whether that is what its own documentation says.
//
// Four places tell an operator that without the key the doors are not mounted:
//
//   config.example.yaml, under factor_key — "Empty — the default — offers no
//       second factor at all: the enrolment routes are not mounted, so signing
//       in stays password-only rather than opening a door that can only answer
//       'unavailable'."
//   kit/config/config.go, on Auth.FactorKey — "Empty means no second factor is
//       offered at all — the enrolment routes are not mounted …"
//   modules/auth/README.md, "A second factor" — "The routes mount only for a
//       deployment that set auth.factor_key … with no key there is no door that
//       can only answer 'unavailable', and signing in stays exactly as it was."
//   modules/auth/internal/factor_routes.go, file comment — "mounted only for a
//       deployment that set a factor key — a route that could only answer 'this
//       installation cannot seal a secret' is a route with nothing to say".
//
// The composition does the opposite, and says why in the sentence beside it
// (modules/auth/module.go, just above svc.EnableFactors): the group is
// registered unconditionally, because "an operation that exists only sometimes
// leaves its four events declared and unreachable, which the application's own
// catalogue check (every declared event has a channel) is right to call a lie".
// Each of the routes declares StatusServiceUnavailable in its own Errors list,
// and factor_routes.go's refusal503 exists only to turn ErrNoFactorKey into that
// answer. surface_test.go agrees: its wholeSurface table carries all six factor
// rows, and the composition it reads sets no key.
//
// So the shape the shipped example ships — factor_key: "" — advertises the six
// factor doors in the served document and answers the four that must seal a
// secret with 503. Whether advertising them or hiding them is right is a call
// somebody still has to make and write in all four places; what no case in this
// repository did was check which of the two the module does. This case does,
// read from outside the module: the kernel's own mount record, the status a
// signed-in caller is answered with, and the half of the prose that is true —
// that a password still signs this person in while the deployment holds no key.
//
// It fails under the change that would make the documentation true (mounting the
// group only when deps.FactorKey is non-empty), and it fails under the change
// that would make a keyless deployment answer 404 by removing refusal503. Either
// way the tree then has to say what it does consistently, which is the point.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestAFactorlessDeploymentAnswersItsFactorDoorsRatherThanHidingThem(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	// No FactorKey in these Deps: this is config.example.yaml's default, the
	// arrangement every installation that follows the example file runs.
	router, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false)

	const email = "ada@acme.localhost"
	person(t, conn, email, contracts.RoleAdmin)

	// The doors, read off the record the kernel writes from inside httpx.prepare
	// rather than off a file name, so a group registered somewhere else in this
	// package would still appear here.
	mounted := map[string]string{}
	for _, m := range api.Mounted() {
		if m.Module == "auth" && strings.HasPrefix(m.Path, "/api/v1/auth/factors") {
			mounted[m.Method+" "+m.Path] = m.Auth
		}
	}
	for _, want := range []string{
		"POST /api/v1/auth/factors/totp/begin",
		"POST /api/v1/auth/factors/totp/finish",
		"GET /api/v1/auth/factors",
		"POST /api/v1/auth/factors/recovery/rotate",
	} {
		if _, ok := mounted[want]; !ok {
			t.Errorf("with no auth.factor_key the composition answers no %s: the deployment hides its "+
				"second-factor doors, which is the other shape, and then the four places that say %q have to "+
				"be reconciled with the routes' own StatusServiceUnavailable declaration (%v)",
				want, "the enrolment routes are not mounted", mounted)
		}
	}

	// The password still works. This is the half of the prose that holds today,
	// and the assertion the other half is supposed to be protecting.
	res := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("a password sign-in at a deployment with no factor key = %d %s, want 200",
			res.Code, res.Body.String())
	}
	cookie := sessionCookie(res)
	if cookie == "" {
		t.Fatal("the password sign-in set no session cookie")
	}

	// The door that would seal a secret answers, and answers 503 with the reason,
	// to that signed-in caller — which is what Errors: StatusServiceUnavailable
	// on the operation declares and refusal503 delivers.
	begin := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/begin", "", withSession(cookie))
	if begin.Code != http.StatusServiceUnavailable {
		t.Errorf("beginning an enrolment where the deployment set no factor key = %d %s, want %d: the door "+
			"is mounted, so it owes the caller the reason it cannot act rather than a %d that says nothing",
			begin.Code, begin.Body.String(), http.StatusServiceUnavailable, http.StatusNotFound)
	}
	if !strings.Contains(begin.Body.String(), "factor key") {
		t.Errorf("the 503 names no factor key: %s", begin.Body.String())
	}

	// And the door that asks for nothing but the caller's own list answers it:
	// an account at a keyless deployment proves nothing beside its password.
	list := call(t, router, http.MethodGet, "/api/v1/auth/factors", "", withSession(cookie))
	if list.Code != http.StatusOK {
		t.Errorf("listing factors where no key is set = %d %s, want 200 with an empty list",
			list.Code, list.Body.String())
	}
	if strings.Contains(list.Body.String(), `"total":1`) {
		t.Errorf("a deployment that cannot seal a secret reports a factor: %s", list.Body.String())
	}
}
