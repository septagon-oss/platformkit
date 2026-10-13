package main

// Two claims about the branch that makes a refusal
// offer a way on and an ask.
//
// 1. "The way on is never the address that refused." The brief's first item
// asks for a door that answers; the delivery replaced the old link with one
// literal, /app/dashboard, and pinned that literal in a page test and in
// e2e/surfaces.spec.ts. Neither case asks the running server whether the
// address the refusal page actually links to answers the refused person. A
// way on that 404s is the same dead end the walkthroughs met, drawn in
// different ink — so this case reads the link off the page, whatever it
// says, and follows it as the refused person would.
//
// 2. "Notified is in the payload because 'sent' is only ever true of the
// notices that were written" (kit/app/access.go, app.AccessRequested). The
// ask's own loop skips the asker when the asker holds role management, and
// what it records is the length of the recipient list. The person who holds
// role management and is refused something else is the case that tells the
// two apart, so this case makes one: a role granting role:manage and
// nothing else.
//
// 3. The limit. The brief asks for one per person and permission; the branch
// wires it and no case posts the fourth ask. A refused ask must write no
// event and no notice, and must leave the refusal standing.
//
// Every case reaches its assertion through the answer the fixed behaviour gives
// — the address on the page, the rows in the trail, the verdict at the door —
// and never through a sentence this branch might or might not print.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// TestTheWayOnARefusalOffersIsAnAddressThatServesTheWorkspace reads the link the
// refusal page puts in front of the refused person and asks the running server
// what it answers. The brief's requirement is a way on; a 404 and a second
// refusal are not one.
func TestTheWayOnARefusalOffersIsAnAddressThatServesTheWorkspace(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	provisionAs(t, cfg, c, acmeTenant(t, cfg), "ines@acme.test", authcontracts.RoleMember)
	member := signIn(t, cfg, acmeHost, "ines@acme.test", adminPass)

	status, ctype, html := askNavigate(t, cfg, member, acmeHost, askDeniedScreen)
	if status != http.StatusForbidden || !strings.Contains(strings.ToLower(ctype), "text/html") {
		t.Fatalf("GET %s as a member with no grant = %d %s, want the refusal page: %s",
			askDeniedScreen, status, ctype, firstLineOf(html))
	}
	back := wayOnOf(t, html)
	if back == askDeniedScreen {
		t.Errorf("the refusal offers the address that refused as its way on")
	}
	got, gotType, body := askNavigate(t, cfg, member, acmeHost, back)
	switch {
	case got == http.StatusNotFound:
		t.Errorf("the refusal's way on %q answers 404: nothing serves it, so the link is the dead end this brief exists to close", back)
	case got == http.StatusForbidden:
		t.Errorf("the refusal's way on %q answers the same refusal again: the person is back where they started", back)
	case got != http.StatusOK || !strings.Contains(strings.ToLower(gotType), "text/html"):
		t.Errorf("the refusal's way on %q = %d %s, want a workspace page for this person: %s", back, got, gotType, firstLineOf(body))
	}
}

// TestAnAskOnlyRecordsTheNoticesItWrote makes the one holder of role management
// in the tenant who is also refused something, asks through their own session,
// and compares what the trail says was sent with what the bells hold.
func TestAnAskOnlyRecordsTheNoticesItWrote(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	// A role that administers the tenant and reads no tasks: whoever holds it is
	// a recipient of every ask in this tenant and, at /app/task/tasks, a person
	// the kernel refuses. Both halves matter: the recipient list includes them,
	// the notice loop skips them.
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost,
		"/api/v1/auth/roles/gatekeeper", `{"permissions":["role:manage"]}`); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("PUT role gatekeeper = %d %s, want the role created", code, body)
	}
	provisionAs(t, cfg, c, acmeTenant(t, cfg), "keeper@acme.test", "gatekeeper")
	keeper := signIn(t, cfg, acmeHost, "keeper@acme.test", adminPass)

	status, _, html := askNavigate(t, cfg, keeper, acmeHost, askDeniedScreen)
	if status != http.StatusForbidden {
		t.Fatalf("GET %s as a holder of role management who may not read tasks = %d, want 403: %s",
			askDeniedScreen, status, firstLineOf(html))
	}
	code, _, posted := askForm(t, cfg, keeper, acmeHost, "/app/access-request",
		"permission=task%3Aread&path=%2Fapp%2Ftask%2Ftasks")
	if code != http.StatusSeeOther {
		t.Fatalf("POST /app/access-request = %d %s, want the 303 the page lands on", code, posted)
	}

	// The bell of the only other holder of role management in this tenant. The
	// ask does not notify the asker of their own ask, so the notices that exist
	// number one.
	written := 0
	eventually(t, "the one notice this ask could write reaches acme's administrator", func() bool {
		code, body := do(t, cfg, admin, http.MethodGet, acmeHost, noticePath, "")
		written = strings.Count(body, "Access requested")
		return code == http.StatusOK && written == 1
	})

	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var notified string
	eventually(t, "security.access_requested reaches the audit trail", func() bool {
		err := owner.QueryRowContext(t.Context(),
			`SELECT coalesce(max(payload->>'notified'), '') FROM audit_events WHERE name = 'security.access_requested'`).Scan(&notified)
		return err == nil && notified != ""
	})
	if notified != "1" {
		t.Errorf("security.access_requested recorded notified=%s while %d notice stands in the tenant's bell: the ask records a delivery it did not make (the asker is in the recipient list and is never told)",
			notified, written)
	}
}

// TestAnAskBeyondTheLimitWritesNothingEmitsNothingAndGrantsNothing posts the ask
// past the per-person, per-permission limit and asks what the fourth one cost:
// an event, a notice, or a grant.
func TestAnAskBeyondTheLimitWritesNothingEmitsNothingAndGrantsNothing(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	provisionAs(t, cfg, c, acmeTenant(t, cfg), "many@acme.test", authcontracts.RoleMember)
	member := signIn(t, cfg, acmeHost, "many@acme.test", adminPass)

	if status, _, _ := askNavigate(t, cfg, member, acmeHost, askDeniedScreen); status != http.StatusForbidden {
		t.Fatalf("GET %s as a member = %d, want the refusal the ask follows", askDeniedScreen, status)
	}
	const limit = 3
	for i := 1; i <= limit; i++ {
		if code, _, body := askForm(t, cfg, member, acmeHost, "/app/access-request",
			"permission=task%3Aread&path=%2Fapp%2Ftask%2Ftasks"); code != http.StatusSeeOther {
			t.Fatalf("ask %d of %d = %d %s, want the ask accepted", i, limit, code, body)
		}
	}
	code, _, body := askForm(t, cfg, member, acmeHost, "/app/access-request",
		"permission=task%3Aread&path=%2Fapp%2Ftask%2Ftasks")
	if code != http.StatusTooManyRequests {
		t.Fatalf("ask %d = %d %s, want 429 from the limit the brief asks for", limit+1, code, body)
	}

	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var asks int
	eventually(t, "the three accepted asks reach the audit trail", func() bool {
		err := owner.QueryRowContext(t.Context(),
			`SELECT count(*) FROM audit_events WHERE name = 'security.access_requested'`).Scan(&asks)
		return err == nil && asks == limit
	})
	if asks != limit {
		t.Errorf("the trail holds %d asks after %d accepted and one refused, want %d: a refused ask emitted something",
			asks, limit, limit)
	}
	if status, _, _ := askNavigate(t, cfg, member, acmeHost, askDeniedScreen); status != http.StatusForbidden {
		t.Errorf("the refused address answers %d after an ask was refused; an ask grants nothing and must leave the refusal standing", status)
	}
}

// wayOnOf is the address the refusal page offers as its way on, read off the
// page rather than from a constant: the last workspace link the document
// carries, which is where the refusal's own link sits — this shell's chrome adds
// no footer, and the ask form precedes it.
func wayOnOf(t *testing.T, html string) string {
	t.Helper()
	all := regexp.MustCompile(`href="(/app[^"]*)"`).FindAllStringSubmatch(html, -1)
	if len(all) == 0 {
		t.Fatalf("the refusal page offers no workspace address at all: %s", html)
	}
	return all[len(all)-1][1]
}

// TestARefusalNamesTheGrantInTheLanguageTheRequestAskedFor is the brief's other
// half of item 1 — "in the request's language" — read off the running server.
// ui/page/catalogue_test.go proves the Portuguese file carries the keys the
// refusal page asks for; no case on this branch showed the page itself speaking
// them. This one asks for a label through the merged catalogue, so the copy it
// expects is the copy the modules ship rather than a second translation written
// into a test.
func TestARefusalNamesTheGrantInTheLanguageTheRequestAskedFor(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	missing := catalogues().Select("pt-PT").Text("permission.task:read", "")
	if missing == "" {
		t.Fatal("no Portuguese words for task:read in the merged catalogue: the case would assert nothing")
	}
	granter := catalogues().Select("pt-PT").Text("permission.role:manage", "")
	if granter == "" {
		t.Fatal("no Portuguese words for role:manage in the merged catalogue: the case would assert nothing")
	}

	provisionAs(t, cfg, c, acmeTenant(t, cfg), "ines.lus@acme.test", authcontracts.RoleMember)
	member := signIn(t, cfg, acmeHost, "ines.lus@acme.test", adminPass)

	req, err := http.NewRequest(http.MethodGet, "http://"+cfg.Server.Addr+askDeniedScreen, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = acmeHost
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "pt-PT")
	res, err := member.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", askDeniedScreen, err)
	}
	defer res.Body.Close()
	page := readAll(t, res)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("GET %s in pt-PT = %d, want the refusal: %s", askDeniedScreen, res.StatusCode, firstLineOf(page))
	}
	for _, want := range []string{`lang="pt-PT"`, missing, granter, "Peça ao seu administrador", "Pedir acesso"} {
		if !strings.Contains(page, want) {
			t.Errorf("the refusal does not carry %q, so this person was answered in a language they did not ask for", want)
		}
	}
	if strings.Contains(page, "What is missing:") {
		t.Errorf("the refusal carries the English part beside a Portuguese one: %s", firstLineOf(page))
	}
}

// TestEveryDeclaredPermissionHasWordsInTheLanguagesThisTenantIsServedIn is the
// coverage case apps/platformkit/catalog.go names as the reason a module cannot
// ship a permission with no copy for it. It walks the composition rather than a
// list written next to the test, so a new module with no catalogue is a red case
// and not a refusal page that reads "task:read" to a Portuguese speaker.
func TestEveryDeclaredPermissionHasWordsInTheLanguagesThisTenantIsServedIn(t *testing.T) {
	_, cfg := configure(t)
	for _, tag := range []string{"pt-PT"} {
		messages := catalogues()
		for _, m := range compose(cfg).modules {
			for _, p := range m.Permissions {
				key := "permission." + p.Key
				said := messages.Select(tag).Text(key, "")
				switch {
				case said == "":
					t.Errorf("%s: no %s catalogue entry for permission %q; a refusal names it to a person served in %s as a key", m.Name, tag, p.Key, tag)
				case said == p.Key:
					t.Errorf("%s: the %s copy for permission %q is the key itself", m.Name, tag, p.Key)
				}
			}
		}
	}
}

// readAll is the whole body of one response.
func readAll(t *testing.T, res *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read the body: %v", err)
	}
	return string(body)
}

// failingReach is the httpx.AskForAccess this branch ships no fake for, so the
// case writes the one it needs: Recipients answers recErr, and Tell answers
// tellErr on call number failOn (1-based). That is how a case asks whether the
// reach's failure is an outage or an empty answer, and whether notices already
// written in the same transaction outlive the failure.
type failingReach struct {
	recipients []uuid.UUID
	recErr     error
	tellErr    error
	failOn     int
	told       int
}

func (f *failingReach) Recipients(context.Context, db.Tx[db.Tenant]) ([]uuid.UUID, error) {
	return f.recipients, f.recErr
}

func (f *failingReach) Tell(context.Context, db.Tx[db.Tenant], httpx.AccessNotice) error {
	f.told++
	if f.tellErr != nil && f.told == f.failOn {
		return f.tellErr
	}
	return nil
}

// TestAnAskWhoseReachFailsAnswersUnavailableAndWritesNothing is decision 0010's
// rule for a port that cannot answer: the person is told the ask could not be
// made, and nothing is left behind. The kernel words it as an outage rather than
// as "nobody can grant this", and the ask must not be in the trail.
func TestAnAskWhoseReachFailsAnswersUnavailableAndWritesNothing(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	reach := &failingReach{recErr: errors.New("the roles table is not answering")}
	c.access = reach
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	provisionAs(t, cfg, c, acmeTenant(t, cfg), "reach@acme.test", authcontracts.RoleMember)
	member := signIn(t, cfg, acmeHost, "reach@acme.test", adminPass)
	if status, _, _ := askNavigate(t, cfg, member, acmeHost, askDeniedScreen); status != http.StatusForbidden {
		t.Fatalf("GET %s = %d, want the refusal the ask follows", askDeniedScreen, status)
	}
	code, _, body := askForm(t, cfg, member, acmeHost, "/app/access-request",
		"permission=task%3Aread&path=%2Fapp%2Ftask%2Ftasks")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("POST /app/access-request with a failing reach = %d %s, want 503: an empty answer must never be read as nobody-to-tell", code, body)
	}
	if strings.Contains(body, "nobody") {
		t.Errorf("the failure reads as a decision about the tenant rather than an outage: %s", firstLineOf(body))
	}

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	if code, body := do(t, cfg, admin, http.MethodGet, acmeHost, noticePath, ""); code != http.StatusOK ||
		strings.Contains(body, "Access requested") {
		t.Errorf("the failed ask left a notice behind: %d %s", code, body)
	}
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var asks int
	if err := owner.QueryRowContext(t.Context(),
		`SELECT count(*) FROM audit_events WHERE name = 'security.access_requested'`).Scan(&asks); err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	if asks != 0 {
		t.Errorf("the trail holds %d asks after one that failed before it reached anybody", asks)
	}
}

// TestAnAskWhoseNoticeFailsLeavesNoHalfWrittenAsk is the same claim on the other
// half of the reach: with two holders of role management in the tenant and the
// second notice failing, the first must not survive either. The delivery's own
// words are that "the notices and the event commit together or not at all"
// (kit/httpx/access.go, AskForAccess.Tell); a bell that shows an ask the trail
// does not have is a page that claims a delivery the transaction dropped.
func TestAnAskWhoseNoticeFailsLeavesNoHalfWrittenAsk(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	reach := &failingReach{tellErr: errors.New("the mailbox is closed"), failOn: 2}
	c.access = reach
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost,
		"/api/v1/auth/roles/gatekeeper", `{"permissions":["role:manage"]}`); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("PUT role gatekeeper = %d %s", code, body)
	}
	// Three holders of role management: the asker, who the ask skips, and two
	// others it writes to. The second of those is the one that fails, so the
	// first is a notice that exists and then must not.
	provisionAs(t, cfg, c, acmeTenant(t, cfg), "keeper2@acme.test", "gatekeeper")
	provisionAs(t, cfg, c, acmeTenant(t, cfg), "keeper3@acme.test", "gatekeeper")
	keeper := signIn(t, cfg, acmeHost, "keeper2@acme.test", adminPass)
	// The reach answers the three holders this tenant has: the asker among them,
	// because that is what the real reach does (modules/user's Holders knows
	// nothing about who is asking) and the ask's own rule is to skip them.
	reach.recipients = []uuid.UUID{
		userIDOf(t, cfg, adminEmail),
		userIDOf(t, cfg, "keeper2@acme.test"),
		userIDOf(t, cfg, "keeper3@acme.test"),
	}

	code, _, body := askForm(t, cfg, keeper, acmeHost, "/app/access-request",
		"permission=task%3Aread&path=%2Fapp%2Ftask%2Ftasks")
	if code == http.StatusSeeOther {
		t.Fatalf("the ask answered the confirmation page although a notice failed (%d calls to Tell): %s", reach.told, body)
	}
	if reach.told < 2 {
		t.Fatalf("the ask wrote %d notices before the failure; this case needs the second one to fail", reach.told)
	}
	if code, body := do(t, cfg, admin, http.MethodGet, acmeHost, noticePath, ""); code != http.StatusOK ||
		strings.Contains(body, "Access requested") {
		t.Errorf("the notice written before the failure survived the rollback: %d %s", code, body)
	}
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var asks int
	if err := owner.QueryRowContext(t.Context(),
		`SELECT count(*) FROM audit_events WHERE name = 'security.access_requested'`).Scan(&asks); err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	if asks != 0 {
		t.Errorf("the trail holds %d asks whose notices were dropped: the event and the notices did not commit together", asks)
	}
}

// TestTheWorkspaceHomeAnswersTheRefusedMemberWithADashboard is the other half of
// The reason the case above has a passing branch: the workspace
// root is the address this composition serves a signed-in person with no grant
// at all. e2e/design-audit.spec.ts names it ['dashboard', '/app']; this asks the
// running server rather than the spec. Nothing here depends on what the refusal
// page links to — the address is written by this case, not read from the page.
func TestTheWorkspaceHomeAnswersTheRefusedMemberWithADashboard(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	provisionAs(t, cfg, c, acmeTenant(t, cfg), "home@acme.test", authcontracts.RoleMember)
	member := signIn(t, cfg, acmeHost, "home@acme.test", adminPass)
	if status, _, _ := askNavigate(t, cfg, member, acmeHost, askDeniedScreen); status != http.StatusForbidden {
		t.Fatalf("GET %s = %d, want this person refused at the task desk first", askDeniedScreen, status)
	}
	status, ctype, body := askNavigate(t, cfg, member, acmeHost, "/app")
	if status != http.StatusOK || !strings.Contains(strings.ToLower(ctype), "text/html") {
		t.Errorf("GET /app as a refused member = %d %s, want the workspace dashboard in a page: %s",
			status, ctype, firstLineOf(body))
	}
}
