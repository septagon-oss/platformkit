package internal_test

// The module's whole HTTP surface, as the kernel recorded it.
//
// The list below is not read out of any source file. It is compared against
// httpx.API.Mounted — the same record the compose gate, the boot line and the
// catalogue read — which every door in the kernel writes from inside
// httpx.prepare. A module therefore cannot add an operation without this case
// seeing it, whichever of this package's seven route files it was written in,
// because the record is made by the mount and not by a file name.
//
// That is the guarantee a route count has to have to be worth pinning: the
// module answers with these 23 operations and nothing else, guard included.
// An operation that appears here unannounced is a new door on a signed-in
// surface, and the person reviewing the diff is the one who has to have
// written its row.
//
// Three compositions are read, because the register door is whichever
// registration policy the composition chose and the two OIDC legs appear only
// when some tenant can reach a provider. A row that stopped being conditional
// would fail the second case, a conditional leg that had quietly become
// permanent would fail it too, and a door belonging to one registration policy
// and not the other would fail the third. The third composition is the one the
// reference application runs — apps/platformkit/modules.go chooses
// EmailRegistration — so the two mailbox doors its sign-up page posts to are
// named here instead of being mounted in a composition no record reads.

import (
	"sort"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// route is one door: which surface answers it, the verb, the address the kernel
// composed, and the authorisation it declares. The guard is part of the row
// because a route that moved from httpx.SignedIn() to httpx.Public() kept its
// address and its verb, and changed what the module is.
type route struct{ surface, method, path, guard string }

func (r route) String() string { return r.surface + " " + r.method + " " + r.path + " " + r.guard }

// wholeSurface is every operation the auth module answers with, in the
// composition that has everything: a registration policy and an installation
// issuer. 29 rows, and one of each of the module's six route groups.
var wholeSurface = []route{
	// handler.go — signing in and out, the caller's own identity, sessions and
	// password, and the two roles routes.
	{"app", "POST", "/api/v1/auth/login", "public"},
	{"app", "POST", "/api/v1/auth/logout", "signed_in"},
	{"app", "GET", "/api/v1/auth/me", "signed_in"},
	{"app", "POST", "/api/v1/auth/password", "signed_in"},
	{"public", "POST", "/api/v1/public/auth/password/forgot", "public"},
	{"app", "POST", "/api/v1/auth/password/reset", "public"},
	{"app", "GET", "/api/v1/auth/sessions", "signed_in"},
	{"app", "POST", "/api/v1/auth/sessions/{ref}/revoke", "signed_in"},
	{"app", "POST", "/api/v1/auth/sessions/revoke-all", "signed_in"},
	{"app", "GET", "/api/v1/auth/roles", "permission role:manage"},
	{"app", "PUT", "/api/v1/auth/roles/{name}", "permission role:manage"},
	// token_routes.go — a person's own keys.
	{"app", "POST", "/api/v1/auth/tokens", "signed_in"},
	{"app", "GET", "/api/v1/auth/tokens", "signed_in"},
	{"app", "POST", "/api/v1/auth/tokens/{id}/revoke", "signed_in"},
	// factor_routes.go — the second factor and its recovery codes.
	{"app", "POST", "/api/v1/auth/factors/totp/begin", "signed_in"},
	{"app", "POST", "/api/v1/auth/factors/totp/finish", "signed_in"},
	{"app", "GET", "/api/v1/auth/factors", "signed_in"},
	{"app", "DELETE", "/api/v1/auth/factors/{id}", "signed_in"},
	{"app", "POST", "/api/v1/auth/factors/recovery/rotate", "signed_in"},
	{"app", "POST", "/api/v1/auth/challenge/verify", "public"},
	// passkey_routes.go — the same second factor, answered by a signature.
	{"app", "POST", "/api/v1/auth/factors/passkey/begin", "signed_in"},
	{"app", "POST", "/api/v1/auth/factors/passkey/finish", "signed_in"},
	{"app", "POST", "/api/v1/auth/challenge/passkey/begin", "public"},
	{"app", "POST", "/api/v1/auth/challenge/passkey/verify", "public"},
	{"app", "POST", "/api/v1/auth/login/passkey/begin", "public"},
	{"app", "POST", "/api/v1/auth/login/passkey/verify", "public"},
	// registration.go — the one register door this composition chose.
	{"public", "POST", "/api/v1/public/auth/register", "public"},
	// oidc.go — the two legs, mounted because the installation has an issuer.
	{"app", "GET", "/api/v1/auth/oidc/start", "public"},
	{"app", "GET", "/api/v1/auth/oidc/callback", "public"},
}

// conditionalLegs are the three rows above that a plain composition does not
// mount: no registration policy, so no register door, and no issuer at all, so
// no door onto one.
var conditionalLegs = map[string]bool{
	route{"public", "POST", "/api/v1/public/auth/register", "public"}.String(): true,
	route{"app", "GET", "/api/v1/auth/oidc/start", "public"}.String():          true,
	route{"app", "GET", "/api/v1/auth/oidc/callback", "public"}.String():       true,
}

// issuerLegs are the two OIDC rows, conditional in every registration policy
// alike: they appear when the installation names an issuer and not otherwise.
var issuerLegs = map[string]bool{
	route{"app", "GET", "/api/v1/auth/oidc/start", "public"}.String():    true,
	route{"app", "GET", "/api/v1/auth/oidc/callback", "public"}.String(): true,
}

// mailboxDoors are the two public operations the email-confirmation policy adds
// beside the /register address it shares with the open policy, and which the
// open policy does not mount at all.
var mailboxDoors = []route{
	{"public", "POST", "/api/v1/public/auth/resend-verification", "public"},
	{"public", "POST", "/api/v1/public/auth/verify-email", "public"},
}

// TestTheMailboxRegistrationPolicyMountsItsOwnDoors reads the composition the
// reference application actually runs: the email-confirmation policy rather than
// the open register door, and no issuer. It answers with 23 operations again, of
// which two are mailbox doors the table above does not carry and the two OIDC
// legs are absent. This is the case that sees a route written into
// email_registration.go, which neither of the other two compositions mounts.
func TestTheMailboxRegistrationPolicyMountsItsOwnDoors(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	_, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false, emailSignup)

	want := make([]route, 0, len(wholeSurface))
	for _, r := range wholeSurface {
		if issuerLegs[r.String()] {
			continue
		}
		want = append(want, r)
	}
	want = append(want, mailboxDoors...)

	got := mountedHere(api)
	if len(got) != len(want) {
		t.Fatalf("the email-confirmation composition mounted %d operations, want %d: %s",
			len(got), len(want), diff(got, want))
	}
	if d := diff(got, want); d != "" {
		t.Fatalf("the mailbox policy moved without this list moving with it: %s", d)
	}
}

func TestTheModuleAnswersWithExactlyTheSurfaceItDeclares(t *testing.T) {
	issuer := authtest.NewIssuer(t)
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	_, _, _, api := mountRecorded(t, conn, auth.OIDC{
		Issuer: issuer.URL, ClientID: "platformkit", ClientSecret: "secret",
		RedirectPath: "/api/v1/auth/oidc/callback",
	}, true)

	got := mountedHere(api)
	if len(got) != len(wholeSurface) {
		t.Fatalf("the composition mounted %d operations, the module declares %d: %s",
			len(got), len(wholeSurface), diff(got, wholeSurface))
	}
	if d := diff(got, wholeSurface); d != "" {
		t.Fatalf("the module's surface moved without this list moving with it: %s", d)
	}
}

// TestTheConditionalLegsAreMountedOnlyByTheCompositionThatWantsThem is the
// other half of the same table: three of its rows belong to a policy or an
// issuer, and a composition with neither must mount 20 operations, not 23.
// Routes that appear and vanish on a deployment's settings are the reason this
// is a composition reading and not a file count.
func TestTheConditionalLegsAreMountedOnlyByTheCompositionThatWantsThem(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	_, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false)

	got := mountedHere(api)
	want := make([]route, 0, len(wholeSurface))
	for _, r := range wholeSurface {
		if conditionalLegs[r.String()] {
			continue
		}
		want = append(want, r)
	}
	if len(got) != len(want) {
		t.Fatalf("a composition with no registration policy and no issuer mounted %d operations, want %d: %s",
			len(got), len(want), diff(got, want))
	}
	if d := diff(got, want); d != "" {
		t.Fatalf("a conditional leg answered without its condition: %s", d)
	}
}

// mountedHere is what the kernel recorded for this module, as table rows.
func mountedHere(api *httpx.API) []route {
	var out []route
	for _, m := range api.Mounted() {
		if m.Module != "auth" {
			continue
		}
		out = append(out, route{string(m.Surface), m.Method, m.Path, m.Auth})
	}
	return out
}

// diff names what one side has that the other does not, as addresses. Both
// sides are sorted, so a reordering of the list above is not reported as a
// change of surface — only an operation appearing or vanishing is.
func diff(mounted []route, declared []route) string {
	have := map[string]bool{}
	for _, r := range mounted {
		have[r.String()] = true
	}
	want := map[string]bool{}
	for _, r := range declared {
		want[r.String()] = true
	}
	var extra, missing []string
	for k := range have {
		if !want[k] {
			extra = append(extra, k)
		}
	}
	for k := range want {
		if !have[k] {
			missing = append(missing, k)
		}
	}
	sort.Strings(extra)
	sort.Strings(missing)
	var parts []string
	if len(extra) > 0 {
		parts = append(parts, "mounted with no row here: "+strings.Join(extra, ", "))
	}
	if len(missing) > 0 {
		parts = append(parts, "declared and never mounted: "+strings.Join(missing, ", "))
	}
	return strings.Join(parts, "; ")
}
