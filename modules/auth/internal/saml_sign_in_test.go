package internal_test

// T-0292's acceptance, at the door it happens at: a person signs in through a SAML
// assertion at a tenant's own host and gets the session any other door opens, and the
// documents the brief names — an unsigned assertion, one addressed to another audience,
// one already presented, one that answers no request this browser made — are each
// refused with nothing written.
//
// The stand-in IdP is `authtest.NewSAMLIdP`, built from the same library the service
// provider is, so the assertion the accepted case accepts is signed by a certificate the
// tenant's own row holds, and the refused cases are refused for their own rule and not
// for a malformed document: the unsigned variant is a valid Response with a valid
// envelope signature and no assertion signature; the wrong-audience variant is a valid
// assertion for somebody else.
//
// The tenant is configured from the IdP's own metadata document, fetched over HTTP from
// it, and the IdP is configured from the tenant's SP metadata, fetched over HTTP from
// the application — the registration an administrator performs in the product, and the
// reason /saml/metadata is in the test path rather than beside it.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// samlEmailAttribute is the attribute the tenant names, spelled the way a Windows
// federation server spells it, because that is the configuration a real tenant writes.
const samlEmailAttribute = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"

// samlACSPath is the assertion consumer service's composed address: the URL an
// assertion's recipient has to name, and the one the SP metadata publishes.
const samlACSPath = "/api/v1/auth/saml/callback"

// mapSAMLProviders is the tenant module's row for the SAML leg: the port is one method,
// so the double is a map — which is the whole reason the port is declared over the
// capability rather than over the tenant module's Service.
type mapSAMLProviders map[uuid.UUID]contracts.SAMLProvider

func (m mapSAMLProviders) ProviderOf(_ context.Context, tx db.Tx[db.Tenant]) (*contracts.SAMLProvider, bool, error) {
	settings, ok := m[db.TenantOf(tx).ID]
	if !ok {
		return nil, false, nil
	}
	return &settings, true, nil
}

// samlFixture is one process, one tenant whose SAML provider is configured from the
// stand-in's own document, and that tenant's SP metadata handed back to the IdP so it
// knows where — and for whom — to write an assertion.
type samlFixture struct {
	conn      *db.Conn
	idp       *authtest.SAMLIdP
	providers mapSAMLProviders
	router    http.Handler
}

// samlSignInUp mounts the module with no installation-level issuer and a SAML provider
// on acme's row only. It returns the mounted router with it, because every case here
// needs the whole leg and none of them needs a second process. The optional mutators
// are how a case that changes one dependency — a factor key, a provisioner — says so
// without rebuilding the federation around it.
func samlSignInUp(t *testing.T, configure ...func(*auth.Deps)) *samlFixture {
	t.Helper()
	idp := authtest.NewSAMLIdP(t)
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)

	fixture := &samlFixture{conn: conn, idp: idp, providers: mapSAMLProviders{acme.ID: {
		EntityID:       "urn:pkit:acme",
		MetadataXML:    idp.MetadataXML(t),
		EmailAttribute: samlEmailAttribute,
	}}}
	idp.SignInAs("ada@acme.localhost", samlEmailAttribute)

	fixture.router, _, _ = mountConfigured(t, conn, auth.OIDC{}, false, func(deps *auth.Deps) {
		deps.SAMLProviders = fixture.providers
		for _, change := range configure {
			change(deps)
		}
	})
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)

	// The registration step, over HTTP: the IdP's administrator downloads this
	// tenant's service provider metadata, which is what names the entity ID the
	// audience must carry and the ACS URL the recipient must name. A test that
	// invented that document would never notice a metadata route that served the
	// wrong host.
	res := call(t, fixture.router, http.MethodGet, "/api/v1/auth/saml/metadata", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /saml/metadata = %d %s, want 200", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/samlmetadata+xml") {
		t.Errorf("the SP metadata is served as %q, want application/samlmetadata+xml", got)
	}
	if !strings.Contains(res.Body.String(), "urn:pkit:acme") {
		t.Errorf("the SP metadata does not name this tenant's entity ID: %s", res.Body.String())
	}
	if strings.Contains(res.Body.String(), "signing") {
		t.Errorf("the SP metadata claims a signing key this installation does not have: %s", res.Body.String())
	}
	descriptor, err := authtest.IdPMetadata(res.Body.String())
	if err != nil {
		t.Fatalf("the SP metadata does not parse: %v", err)
	}
	idp.Trust("urn:pkit:acme", descriptor)
	return fixture
}

// start begins a sign-in the way a browser does, and returns the URL the browser was
// sent to and the cookie that remembers which request went out.
func (f *samlFixture) start(t *testing.T, at string) (location, cookie string) {
	t.Helper()
	res := get(t, f.router, at, "/api/v1/auth/saml/start")
	if res.Code != http.StatusSeeOther {
		t.Fatalf("%s /saml/start = %d %s, want 303", at, res.Code, res.Body.String())
	}
	location = res.Header().Get("Location")
	if !strings.Contains(location, "SAMLRequest=") {
		t.Fatalf("the start redirect carries no SAMLRequest: %s", location)
	}
	for _, c := range (&http.Response{Header: res.Header()}).Cookies() {
		if strings.Contains(c.Name, "saml") {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("start set no request-tracking cookie, so no answer to it could be recognised")
	}
	return location, cookie
}

// present drives the IdP with the URL start sent the browser to, and answers the ACS
// address it built its response for and the base64 document itself.
func (f *samlFixture) present(t *testing.T, location string) (acs, response string) {
	t.Helper()
	res, err := http.Get(location)
	if err != nil {
		t.Fatalf("driving the identity provider: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading the identity provider's answer: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the identity provider answered %d: %s", res.StatusCode, body)
	}
	lines := strings.SplitN(string(body), "\n", 2)
	if len(lines) != 2 || lines[1] == "" {
		t.Fatalf("the identity provider answered %q, want an ACS URL and a response", body)
	}
	return lines[0], lines[1]
}

// signInTo returns the whole sign-in as one call, so a case that only changes the
// document has to say so and nothing else.
func (f *samlFixture) signInTo(t *testing.T, at string) *httptest.ResponseRecorder {
	t.Helper()
	location, cookie := f.start(t, at)
	acs, response := f.present(t, location)
	return post(t, f.router, at, acs, response, cookie)
}

// post is the browser's POST to the assertion consumer service: form-encoded, carrying
// the cookie the start leg set and nothing else — no session, no CSRF token, because
// the person posting it is not signed in yet, which is exactly why the assertion has to
// answer a request this browser made.
func post(t *testing.T, router http.Handler, at, acs, response, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"SAMLResponse": {response}}
	target := "http://" + at + samlACSPath
	if acs != "" {
		target = acs
	}
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: httpx.CookieName("platformkit_saml_request", false), Value: cookie})
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// tally is the three facts a refusal must leave untouched: sessions, spent assertions,
// and the trail's logins.
type tally struct{ sessions, replays, logins int }

// counts reads them, in this tenant's own transaction, over the real tables.
func (f *samlFixture) counts(t *testing.T) tally {
	t.Helper()
	var sessions, replays, logins int64
	err := db.Run(tenancy.WithTenant(t.Context(), acme), f.conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := tx.DB().Table("sessions").Count(&sessions).Error; err != nil {
				return err
			}
			if err := tx.DB().Table("saml_assertion_replays").Count(&replays).Error; err != nil {
				return err
			}
			return tx.DB().Raw(`SELECT count(*) FROM platformkit_outbox WHERE name = $1`,
				contracts.EventLoggedIn).Scan(&logins).Error
		})
	if err != nil {
		t.Fatalf("counting what the leg wrote: %v", err)
	}
	return tally{int(sessions), int(replays), int(logins)}
}

func TestASAMLSignInOpensTheSameSessionAsAnyOtherDoor(t *testing.T) {
	f := samlSignInUp(t)
	before := f.counts(t)

	res := f.signInTo(t, host)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("the assertion consumer service = %d %s, want 303", res.Code, res.Body.String())
	}
	if res.Header().Get("Location") != "/" {
		t.Errorf("the sign-in redirects to %q, want the application's own root", res.Header().Get("Location"))
	}
	session := sessionCookie(res)
	if session == "" {
		t.Fatal("the sign-in set no session cookie")
	}
	if me := call(t, f.router, http.MethodGet, "/api/v1/auth/me", "", withSession(session)); me.Code != http.StatusOK ||
		!strings.Contains(me.Body.String(), "ada@acme.localhost") {
		t.Errorf("me after a SAML sign-in = %d %s", me.Code, me.Body.String())
	}

	got := f.counts(t)
	if got.sessions != before.sessions+1 {
		t.Errorf("the sign-in left %d sessions on top of %d, want one more", got.sessions, before.sessions)
	}
	if got.replays != 1 {
		t.Errorf("the sign-in left %d spent assertion rows, want one", got.replays)
	}
	if got.logins != before.logins+1 {
		t.Errorf("the sign-in published %d logins on top of %d, want one", got.logins, before.logins)
	}
	// The trail names the door. A SAML sign-in the log called "oidc" would send
	// whoever is reading it to the wrong provider about the person they are asking
	// about, which is the whole reason Open asks for the method.
	var method string
	err := db.Run(tenancy.WithTenant(t.Context(), acme), f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT payload->>'method' FROM platformkit_outbox WHERE name = $1
			ORDER BY created_at DESC LIMIT 1`, contracts.EventLoggedIn).Scan(&method).Error
	})
	if err != nil {
		t.Fatalf("reading the login's method: %v", err)
	}
	if method != string(contracts.ViaSAML) {
		t.Errorf("the trail says the sign-in came through %q, want %q", method, contracts.ViaSAML)
	}
}

func TestAnUnsignedSAMLAssertionIsRefused(t *testing.T) {
	f := samlSignInUp(t)
	before := f.counts(t)
	// The IdP signs the Response envelope and not the assertion inside it: the
	// document a real IdP with assertion signing switched off sends, and the one the
	// library's artifact-binding entry point would accept.
	f.idp.UnsignedAssertion()

	res := f.signInTo(t, host)
	if res.Code != http.StatusForbidden {
		t.Fatalf("an unsigned assertion = %d %s, want 403", res.Code, res.Body.String())
	}
	if sessionCookie(res) != "" {
		t.Error("the refusal still set a session cookie")
	}
	assertNothingWritten(t, f, before)
}

func TestASAMLAssertionForAnotherAudienceIsRefused(t *testing.T) {
	f := samlSignInUp(t)
	before := f.counts(t)
	f.idp.AudienceIs("urn:pkit:globex")

	res := f.signInTo(t, host)
	if res.Code != http.StatusForbidden {
		t.Fatalf("an assertion addressed to another tenant's entity ID = %d %s, want 403",
			res.Code, res.Body.String())
	}
	assertNothingWritten(t, f, before)
}

func TestASAMLAssertionAddressedToAnotherHostIsRefused(t *testing.T) {
	f := samlSignInUp(t)
	before := f.counts(t)
	// The audience stays right and only the recipient moves: the two refusals are
	// separate rules, and a case that broke both could not tell them apart.
	f.idp.RecipientIs("http://other.localhost/api/v1/auth/saml/callback")

	res := f.signInTo(t, host)
	if res.Code != http.StatusForbidden {
		t.Fatalf("an assertion naming another host's ACS = %d %s, want 403", res.Code, res.Body.String())
	}
	assertNothingWritten(t, f, before)
}

// TestASAMLAssertionWithNoAudienceRestrictionIsRefused is the forwarded assertion at
// its cheapest: not an audience that names another tenant — the case above — but no
// audience at all, in a document a trusted IdP signed. A reader that takes an empty list
// of restrictions as "nothing to check" accepts it at every tenant on one installation
// at once, which is the property this refusal takes away.
func TestASAMLAssertionWithNoAudienceRestrictionIsRefused(t *testing.T) {
	f := samlSignInUp(t)
	before := f.counts(t)
	f.idp.NoAudienceRestriction()

	res := f.signInTo(t, host)
	if res.Code != http.StatusForbidden {
		t.Fatalf("an assertion naming no audience = %d %s, want 403", res.Code, res.Body.String())
	}
	if sessionCookie(res) != "" {
		t.Error("the refusal still set a session cookie")
	}
	assertNothingWritten(t, f, before)
}

// TestASAMLAssertionBoundToNoBearerIsRefused is the same document with its subject cut
// down to a bare name: still signed, still addressed here, and binding itself to nobody
// who could present it. The library checks Recipient and the request an assertion answers
// for every confirmation a subject carries, and for none when it carries no confirmation
// at all, so this refusal is the installation's own.
func TestASAMLAssertionBoundToNoBearerIsRefused(t *testing.T) {
	f := samlSignInUp(t)
	before := f.counts(t)
	f.idp.NoSubjectConfirmation()

	res := f.signInTo(t, host)
	if res.Code != http.StatusForbidden {
		t.Fatalf("an assertion confirming no bearer = %d %s, want 403", res.Code, res.Body.String())
	}
	if sessionCookie(res) != "" {
		t.Error("the refusal still set a session cookie")
	}
	assertNothingWritten(t, f, before)
}

func TestASAMLAssertionPresentedTwiceIsRefused(t *testing.T) {
	f := samlSignInUp(t)
	location, cookie := f.start(t, host)
	acs, response := f.present(t, location)

	if first := post(t, f.router, host, acs, response, cookie); first.Code != http.StatusSeeOther {
		t.Fatalf("the first presentation = %d %s, want 303", first.Code, first.Body.String())
	}
	second := post(t, f.router, host, acs, response, cookie)
	if second.Code != http.StatusForbidden {
		t.Fatalf("the same bytes presented again = %d %s, want 403", second.Code, second.Body.String())
	}
	got := f.counts(t)
	if got.sessions != 1 {
		t.Errorf("one assertion opened %d sessions, want one", got.sessions)
	}
	if got.replays != 1 {
		t.Errorf("one assertion left %d spent rows, want one", got.replays)
	}
	if got.logins != 1 {
		t.Errorf("one assertion published %d logins, want one", got.logins)
	}
}

func TestAnIdPInitiatedSAMLAssertionIsRefused(t *testing.T) {
	f := samlSignInUp(t)
	before := f.counts(t)
	// A valid, correctly addressed assertion, posted by a browser that never started
	// the sign-in here. The kernel's CSRF gate cannot cover this POST — an anonymous
	// cross-site POST carries no session cookie to compare against — so the assertion
	// has to name a request this very browser registered, and one that names none is
	// refused whatever its signature says.
	location, _ := f.start(t, host)
	_, response := f.present(t, location)

	res := post(t, f.router, host, "", response, "")
	if res.Code != http.StatusForbidden {
		t.Fatalf("an assertion posted with no start cookie = %d %s, want 403", res.Code, res.Body.String())
	}
	assertNothingWritten(t, f, before)
}

func TestAnExpiredSAMLAssertionIsRefusedAndOneInsideTheSkewIsAccepted(t *testing.T) {
	f := samlSignInUp(t)
	outside := f.counts(t)
	// The library's own tolerance is 180 seconds; the row this writes expires at the
	// same moment the document stops being presentable, so the window is the rule and
	// not a suggestion. Both sides are pinned because either half alone passes a
	// regression that widens the other.
	f.idp.Window(time.Now().Add(-time.Hour), time.Now().Add(-240*time.Second))
	if res := f.signInTo(t, host); res.Code != http.StatusForbidden {
		t.Fatalf("an assertion 240 s past its NotOnOrAfter = %d %s, want 403", res.Code, res.Body.String())
	}
	assertNothingWritten(t, f, outside)

	f.idp.Window(time.Now().Add(-time.Hour), time.Now().Add(-120*time.Second))
	if res := f.signInTo(t, host); res.Code != http.StatusSeeOther {
		t.Fatalf("the same assertion 120 s past its NotOnOrAfter, inside the 180 s skew = %d %s, want 303",
			res.Code, res.Body.String())
	}
}

func TestSAMLUnknownAddressRefusesAndDisabledTenantDialsNobody(t *testing.T) {
	f := samlSignInUp(t)
	before := f.counts(t)
	f.idp.SignInAs("grace@elsewhere.example", samlEmailAttribute)

	res := f.signInTo(t, host)
	if res.Code != http.StatusForbidden {
		t.Fatalf("an address this tenant has no account for = %d %s, want 403", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "no account here") {
		t.Errorf("the refusal says %q, want the sentence that says nobody is made here", res.Body.String())
	}
	assertNothingWritten(t, f, before)

	// `disabled` refuses at the door, before the metadata is parsed and before the
	// IdP is dialed at all — which is what the dial counter is for.
	f.providers[acme.ID] = contracts.SAMLProvider{
		EntityID: "urn:pkit:acme", MetadataXML: f.providers[acme.ID].MetadataXML,
		EmailAttribute: samlEmailAttribute, Registration: contracts.RegistrationDisabled,
	}
	started := get(t, f.router, host, "/api/v1/auth/saml/start")
	if started.Code != http.StatusForbidden {
		t.Fatalf("a tenant that turned SAML off answered /saml/start with %d, want 403", started.Code)
	}
}

func TestTheSAMLLegsAreMountedOnlyWhenAPortAnswersThem(t *testing.T) {
	// The same composition with no SAMLProviders at all: not three doors that answer
	// 404 for everybody, and no row in the declared surface for a module that cannot
	// resolve a tenant's IdP.
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false)

	for _, path := range []string{"/api/v1/auth/saml/start", samlACSPath, "/api/v1/auth/saml/metadata"} {
		if res := call(t, router, http.MethodGet, path, ""); res.Code == http.StatusNotFound {
			continue
		} else if res.Code == http.StatusMethodNotAllowed {
			continue
		} else {
			t.Errorf("%s answers %d on a composition with no SAML provider; the legs are mounted by the port",
				path, res.Code)
		}
	}
}

// assertNothingWritten is the half every refusal has to hold: a 403 that opened a
// session, spent an assertion or published a login is a refusal that changed the world.
func assertNothingWritten(t *testing.T, f *samlFixture, before tally) {
	t.Helper()
	got := f.counts(t)
	if got != before {
		t.Errorf("the refusal wrote: sessions %+v->%+d, spent assertions %+v->%+d, logins %+v->%+d",
			before, got.sessions, before, got.replays, before, got.logins)
	}
}

// TestTwoBrowsersPresentingOneAssertionAtOnceMakeExactlyOneSession races the two
// presentations the replay rule exists for: one assertion, two POSTs, at the same
// moment. The second cannot be turned away by a read — nothing is committed yet for
// either of them to see — so the decision is the primary key's: the loser blocks on
// the unique index until the winner's transaction ends, then finds the pair present
// and refuses. Run concurrently enough to be worth running, because the case a
// sequential test proves is the case the second presentation already proved above.
func TestTwoBrowsersPresentingOneAssertionAtOnceMakeExactlyOneSession(t *testing.T) {
	f := samlSignInUp(t)
	before := f.counts(t)
	location, cookie := f.start(t, host)
	acs, response := f.present(t, location)

	startLine, settled := make(chan struct{}), sync.WaitGroup{}
	codes := make([]int, 2)
	for i := range codes {
		settled.Add(1)
		go func(i int) {
			defer settled.Done()
			<-startLine
			codes[i] = post(t, f.router, host, acs, response, cookie).Code
		}(i)
	}
	close(startLine)
	settled.Wait()

	signedIn := 0
	for _, code := range codes {
		if code == http.StatusSeeOther {
			signedIn++
		}
	}
	if signedIn != 1 {
		t.Errorf("one assertion presented twice at once opened %d sign-ins (%v), want exactly one: "+
			"the primary key is the claim, and a race that lets both through is two sessions for one vouch",
			signedIn, codes)
	}
	got := f.counts(t)
	if got.sessions != before.sessions+1 {
		t.Errorf("the race left %d sessions on top of %d, want one", got.sessions, before.sessions)
	}
	if got.replays != before.replays+1 {
		t.Errorf("the race left %d spent rows on top of %d, want one", got.replays, before.replays)
	}
	if got.logins != before.logins+1 {
		t.Errorf("the race published %d logins on top of %d, want one", got.logins, before.logins)
	}
}

// TestAFederatedSAMLSignInRefusedForItsSecondHalfFinishesAtTheChallenge walks the
// brief's "the same session and second-factor rules as an OIDC one, through the same
// function" at the door SAML arrives at. The person enrols a factor over the routes —
// the account's state, not a test's arrangement — and the SAML leg then hands the
// person the same refusal the password leg hands them, with the same way out.
//
// The refused leg is also the case that pins what a refusal spends: the assertion was
// already claimed a moment earlier in the same transaction, and the 401 rolls that
// claim back with everything else. Burn the assertion on a sign-in that never opened
// anything and anyone holding the bytes could lock the real person out of their own
// account, which is why the spent-row count below is read and not assumed.
func TestAFederatedSAMLSignInRefusedForItsSecondHalfFinishesAtTheChallenge(t *testing.T) {
	f := samlSignInUp(t, func(deps *auth.Deps) { deps.FactorKey = "a factor key this deployment set" })
	first := sessionCookie(f.signInTo(t, host))
	if first == "" {
		t.Fatal("the SAML leg signed nobody in before a factor existed, so nothing below tests the factor")
	}
	begin := call(t, f.router, http.MethodPost, "/api/v1/auth/factors/totp/begin", "", withSession(first))
	if begin.Code != http.StatusOK {
		t.Fatalf("beginning an enrolment = %d %s, want 200", begin.Code, begin.Body.String())
	}
	var enrolment contracts.TOTPEnrolment
	if err := json.Unmarshal(begin.Body.Bytes(), &enrolment); err != nil || enrolment.Secret == "" {
		t.Fatalf("the enrolment showed no secret: %v (%s)", err, begin.Body.String())
	}
	code := codeFor(t, enrolment.Secret, db.Now())
	if finish := call(t, f.router, http.MethodPost, "/api/v1/auth/factors/totp/finish",
		`{"secret":"`+enrolment.Secret+`","code":"`+code+`"}`, withSession(first)); finish.Code != http.StatusCreated {
		t.Fatalf("finishing an enrolment = %d %s, want 201", finish.Code, finish.Body.String())
	}

	before := f.counts(t)
	held := f.signInTo(t, host)
	if held.Code != http.StatusUnauthorized {
		t.Fatalf("the SAML leg for a person holding a factor = %d %s, want 401",
			held.Code, held.Body.String())
	}
	if !strings.Contains(held.Body.String(), "second factor") {
		t.Errorf("the refusal says nothing about what is missing: %s", held.Body.String())
	}
	if sessionCookie(held) != "" {
		t.Error("the refused leg set a session cookie")
	}
	got := f.counts(t)
	if got.sessions != before.sessions || got.logins != before.logins {
		t.Errorf("the refused leg opened %d sessions and published %d logins on top of %+v, want neither: "+
			"a sign-in refused for its second half is not a login",
			got.sessions-before.sessions, got.logins-before.logins, before)
	}
	if got.replays != before.replays {
		t.Errorf("the refused leg spent %d assertions, want none: the claim commits with the session it "+
			"pays for or not at all, or a refused attempt lets anyone burn a person's sign-in",
			got.replays-before.replays)
	}

	challenge := call(t, f.router, http.MethodPost, "/api/v1/auth/challenge/verify",
		`{"email":"ada@acme.localhost","code":"`+codeFor(t, enrolment.Secret, db.Now())+`"}`)
	if challenge.Code != http.StatusOK {
		t.Fatalf("answering the challenge = %d %s, want 200", challenge.Code, challenge.Body.String())
	}
	finished := sessionCookie(challenge)
	if finished == "" {
		t.Fatal("the challenge set no session cookie")
	}
	if me := call(t, f.router, http.MethodGet, "/api/v1/auth/me", "", withSession(finished)); me.Code != http.StatusOK ||
		!strings.Contains(me.Body.String(), "ada@acme.localhost") {
		t.Errorf("the session the challenge opened = %d %s at /auth/me, want 200 for ada@acme.localhost",
			me.Code, me.Body.String())
	}
}

// TestASAMLAddressWithNoAccountHereIsMadeWhenTheTenantAllowsIt runs the third
// registration mode at the SAML door: the tenant that says an address its provider
// vouched for is on its own an account gets that person made, with the roles its row
// named and nothing else. The refusal arm is a case above; this is the arm that has to
// work, and the roles are read back from the user module rather than from the response.
func TestASAMLAddressWithNoAccountHereIsMadeWhenTheTenantAllowsIt(t *testing.T) {
	const zoe = "zoe@acme.localhost"
	f := samlSignInUp(t, func(deps *auth.Deps) { deps.Provisioner = maker{users: realUsers()} })
	f.providers[acme.ID] = contracts.SAMLProvider{
		EntityID: "urn:pkit:acme", MetadataXML: f.idp.MetadataXML(t), EmailAttribute: samlEmailAttribute,
		Registration: contracts.RegistrationProvision, Roles: []string{contracts.RoleMember},
	}
	f.idp.SignInAs(zoe, samlEmailAttribute)

	res := f.signInTo(t, host)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("a verified address at a tenant that allows provisioning = %d %s, want 303 with a session",
			res.Code, res.Body.String())
	}
	if sessionCookie(res) == "" {
		t.Fatal("the provisioned person got no session cookie")
	}
	var roles []string
	err := db.Run(tenancy.WithTenant(t.Context(), acme), f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		made, err := realUsers().ByEmail(ctx, tx, zoe)
		if err != nil {
			return err
		}
		roles = made.Roles
		return nil
	})
	if err != nil {
		t.Fatalf("reading the person the leg made: %v", err)
	}
	if len(roles) != 1 || roles[0] != contracts.RoleMember {
		t.Errorf("the provisioned person holds %v, want exactly [%s]: the roles the row named and nothing else",
			roles, contracts.RoleMember)
	}
}
