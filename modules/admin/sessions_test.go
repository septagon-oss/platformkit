package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/admin"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// sessionStore is the auth module's session list and its two revocations, in
// memory. It keeps the one rule the real commands are tested on — a revocation
// can only ever reach a session belonging to the caller the command was handed —
// because the screen is a door onto those commands, and a stand-in that revoked
// anything for anybody could not show that the door asks for the caller's own id.
type sessionStore struct {
	items []*authcontracts.SessionListing
	// ids is which session each ref stands for, so a case can ask whether the
	// page kept the one that was asking.
	ids map[string]uuid.UUID
	// caller is whose id the last command arrived with, and except whose
	// session the "everywhere else" write was told to keep.
	caller, except uuid.UUID
	revoked        []string
}

func (s *sessionStore) Sessions(_ context.Context, _ db.Tx[db.Tenant], userID, current uuid.UUID) ([]*authcontracts.SessionListing, error) {
	s.caller, s.except = userID, current
	return s.items, nil
}

func (s *sessionStore) RevokeSession(_ context.Context, _ db.Tx[db.Tenant], userID uuid.UUID, ref string) error {
	s.caller = userID
	for i, item := range s.items {
		if item.Ref != ref {
			continue
		}
		s.revoked = append(s.revoked, ref)
		s.items = append(s.items[:i], s.items[i+1:]...)
		return nil
	}
	return crud.ErrNotFound
}

func (s *sessionStore) RevokeSessions(_ context.Context, _ db.Tx[db.Tenant], userID, except uuid.UUID) error {
	s.caller, s.except = userID, except
	var kept []*authcontracts.SessionListing
	for _, item := range s.items {
		if s.ids[item.Ref] == except {
			kept = append(kept, item)
			continue
		}
		s.revoked = append(s.revoked, item.Ref)
	}
	s.items = kept
	return nil
}

// withSessions composes the shell over the store. Nil mounts no screen at all,
// which is what every composition without the auth module gets, and what the
// roles screen is tested on too.
func withSessions(store *sessionStore) func(*admin.Deps) {
	return func(d *admin.Deps) { d.Sessions = store }
}

// here is the session the harness's own cookie names: the screen's fixture for
// "the one you are reading on".
var here = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func threeSessions() *sessionStore {
	at := time.Now().UTC()
	store := &sessionStore{ids: map[string]uuid.UUID{
		"ref-here":   here,
		"ref-there":  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		"ref-silent": uuid.MustParse("33333333-3333-3333-3333-333333333333"),
	}}
	store.items = []*authcontracts.SessionListing{
		{Ref: "ref-here", UserAgent: "Firefox on a laptop", IP: "203.0.113.9",
			CreatedAt: at.Add(-2 * time.Hour), LastSeenAt: at.Add(-time.Minute),
			ExpiresAt: at.Add(6 * time.Hour), Current: true},
		{Ref: "ref-there", UserAgent: "Safari on a phone", IP: "198.51.100.4",
			CreatedAt: at.Add(-30 * time.Hour), LastSeenAt: at.Add(-3 * time.Hour),
			ExpiresAt: at.Add(48 * time.Hour)},
		{Ref: "ref-silent", CreatedAt: at.Add(-9 * time.Hour), LastSeenAt: at.Add(-9 * time.Hour),
			ExpiresAt: at.Add(15 * time.Hour)},
	}
	return store
}

// signedInAs is a request that carries a session cookie holding a real session
// id — the harness's own `callAt` presents one whose value is the word
// "present", which is enough for its identity hook and is not a session id the
// screen could compare a row against.
func signedInAs(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: here.String()})
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestTheSessionsScreenListsWhatItsCommandsReport is the brief's third item as
// a page: a person sees the machines they are signed in on in their own words,
// and can tell the one they are reading from the others.
func TestTheSessionsScreenListsWhatItsCommandsReport(t *testing.T) {
	store := threeSessions()
	router := mountAs(t, caller{}, withSessions(store))

	res := signedInAs(t, router, http.MethodGet, "/app/auth/sessions", "")
	code, body := res.Code, res.Body.String()
	if code != http.StatusOK {
		t.Fatalf("the sessions screen = %d %s", code, body)
	}
	for _, want := range []string{"Firefox on a laptop", "Safari on a phone", "This device"} {
		if !strings.Contains(body, want) {
			t.Errorf("the screen does not say %q", want)
		}
	}
	// A device that would not identify itself is still a session, and says so
	// rather than rendering a blank row somebody has to guess about.
	if !strings.Contains(body, "would not say what it is") {
		t.Error("a session with no user agent rendered as an empty row")
	}
	// The person reads a device, not a credential: the ref is a form's hidden
	// value and never the row's headline. See contracts.SessionListing.
	if strings.Contains(body, ">ref-there<") {
		t.Error("a ref is shown as the row's text")
	}
	if !strings.Contains(body, `action="/app/auth/sessions/revoke"`) {
		t.Error("the screen renders no form that ends one session")
	}
	if !strings.Contains(body, `action="/app/auth/sessions/revoke-rest"`) {
		t.Error("the screen offers no way to end every other session")
	}
	// Two of the three rows are not this one, so the button says two.
	if !strings.Contains(body, "End the other 2") {
		t.Error("the count of the other sessions is wrong or missing")
	}
	// Where it was opened from, and when it was last used: that is what
	// "was that me?" is answered with.
	if !strings.Contains(body, "198.51.100.4") || !strings.Contains(body, "UTC") {
		t.Error("a row says neither where it was opened nor when it was last used")
	}
	// The screen asked for the caller from the credential, not from the URL.
	if store.caller == uuid.Nil {
		t.Error("the screen read nobody: it passed no caller to the module")
	}
}

// TestTheSessionsScreenEndsTheSessionTheFormNamed is the write and the
// ownership rule that comes with it: the ref comes from the form, the caller
// from the credential, and a ref that is not this person's is the same 404 as
// one that was never there.
func TestTheSessionsScreenEndsTheSessionTheFormNamed(t *testing.T) {
	store := threeSessions()
	router := mountAs(t, caller{}, withSessions(store))

	res := signedInAs(t, router, http.MethodPost, "/app/auth/sessions/revoke", "ref=ref-there")
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/app/auth/sessions" {
		t.Fatalf("ending one session = %d to %q: %s", res.Code, res.Header().Get("Location"), res.Body.String())
	}
	if len(store.revoked) != 1 || store.revoked[0] != "ref-there" {
		t.Fatalf("the screen revoked %v, want the one ref the form carried", store.revoked)
	}
	if store.caller == uuid.Nil {
		t.Error("the write went through with no caller")
	}

	// An unknown ref is a 404 on the page too, and not a redirect that pretends
	// the click worked: the person asked for one machine to be signed out.
	if res = signedInAs(t, router, http.MethodPost, "/app/auth/sessions/revoke", "ref=not-yours"); res.Code != http.StatusNotFound {
		t.Fatalf("a ref that is nobody's = %d: %s", res.Code, res.Body.String())
	}
	if len(store.items) != 2 {
		t.Fatalf("a ref nobody holds changed the list: %d rows are left, want the two that were", len(store.items))
	}

	// Everywhere-but-here keeps the session that is answering, so the click
	// cannot sign the person out mid-page and hand them a 401 for their own
	// list. The row they are reading is ended by its own button, deliberately.
	store.revoked = nil
	if res = signedInAs(t, router, http.MethodPost, "/app/auth/sessions/revoke-rest", ""); res.Code != http.StatusSeeOther {
		t.Fatalf("ending the other sessions = %d: %s", res.Code, res.Body.String())
	}
	if store.except != here {
		t.Errorf("the page kept session %s, want the one the request's own cookie names", store.except)
	}
	for _, ref := range store.revoked {
		if ref == "ref-here" {
			t.Error("the screen ended the session that was answering the request")
		}
	}
	if len(store.items) != 1 || store.items[0].Ref != "ref-here" {
		t.Errorf("%d rows are left, want the one that was asking", len(store.items))
	}
}

// TestTheSessionsScreenSendsARevokedSessionToTheSignInPage is T-0094's failure,
// asserted in the package that owns the sign-in page's address.
//
// The mechanism was never missing: kit/httpx redirects an anonymous HTML GET to
// the path the operation named and ui/page's Serve names the shell's sign-in for
// every page it mounts. What failed was a page that named nothing, or named the
// API route. So the assertion is the Location and not the status: a 303 to
// /api/v1/auth/login is a JSON route a browser cannot sign in through, and it is
// indistinguishable from the working redirect until somebody follows it.
func TestTheSessionsScreenSendsARevokedSessionToTheSignInPage(t *testing.T) {
	router := mountAs(t, caller{}, withSessions(threeSessions()))

	// No cookie at all: the request a browser makes once the session it was
	// holding has been revoked — from another tab, by the sweep, or by the
	// person themselves on the screen above.
	req := httptest.NewRequest(http.MethodGet, "http://"+host+"/app/auth/sessions", nil)
	// What a browser sends. The kernel redirects an anonymous caller to the
	// sign-in page only for a caller that asked for HTML: a program that got a
	// 303 where it expected a 403 has to guess what happened, so the JSON
	// routes keep problem+json and this page, which is for a browser, is the
	// one that gets the redirect.
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("an anonymous GET of the sessions screen = %d, want 303 to the sign-in page: %s",
			w.Code, w.Body.String())
	}
	to := w.Header().Get("Location")
	if !strings.HasPrefix(to, "/app/admin/login") {
		t.Fatalf("a revoked session lands on %q, want the shell's own sign-in page", to)
	}
	// The page they were reading survives the redirect, so signing in again
	// arrives back at the list rather than at the dashboard.
	if !strings.Contains(to, "next=%2Fapp%2Fauth%2Fsessions") {
		t.Errorf("the sign-in page is not told where to go back to: %q", to)
	}
}

// TestTheSessionsScreenDeclaresItsGuard is the same claim the architecture gate
// makes of the JSON routes, read from the recording: the page declares SignedIn
// and no permission, because it answers for the caller themselves — a permission
// there would be a permission for reading one's own sessions, which is not a
// thing a role grants or withholds.
func TestTheSessionsScreenDeclaresItsGuard(t *testing.T) {
	api, _ := mountWithAPI(t, caller{}, withSessions(threeSessions()))
	want := map[string]httpx.Auth{
		"/app/auth/sessions":             httpx.SignedIn(),
		"/app/auth/sessions/revoke":      httpx.SignedIn(),
		"/app/auth/sessions/revoke-rest": httpx.SignedIn(),
	}
	seen := map[string]bool{}
	for _, op := range api.Recorded() {
		expected, ok := want[op.Path]
		if !ok {
			continue
		}
		seen[op.Path] = true
		got, _ := op.Extensions[httpx.AuthExtension].(httpx.Auth)
		if got != expected {
			t.Errorf("%s %s declares %v, want %v", op.Method, op.Path, got, expected)
		}
	}
	for path := range want {
		if !seen[path] {
			t.Errorf("no operation was recorded at %s", path)
		}
	}
}

// TestASessionlessCompositionMountsNoSessionsScreen is the other half of the
// wiring: a composition with no auth module has no screen and no link.
func TestASessionlessCompositionMountsNoSessionsScreen(t *testing.T) {
	router := mountAs(t, caller{})
	if code, body, _ := call(t, router, http.MethodGet, "/app/auth/sessions", ""); code != http.StatusNotFound {
		t.Fatalf("a shell with no Sessions capability answers %d at the sessions path: %s", code, body)
	}
}
