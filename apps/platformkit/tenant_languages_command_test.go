package main

// The tenant's own languages, written by its own administrator, read back from the
// three places a write is supposed to leave a mark: the row, the outbox and the
// audit trail.
//
// The page is the door (apps/platformkit/tenant_languages_page_test.go, the
// reviewer's, opens it); this is the command behind it. What it pins is that a
// tenant administrator's declaration is the same kind of fact as an operator's —
// same tenant.locale_set, same audit record with the actor who asked, same
// refusal-that-writes-nothing — and that the revision on the form is a real
// expected revision rather than a number the page printed.

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// postLanguages submits the languages page as the person's browser does: a form, a
// Referer, the session cookie the sign-in client already carries.
func postLanguages(t *testing.T, cfg config.Config, client *http.Client, revision string) (int, string) {
	t.Helper()
	return postLanguagesForm(t, cfg, client, url.Values{
		"default": {"pt-PT"}, "supported": {"en"}, "localeRevision": {revision},
	})
}

// postOtherLanguages submits the *other* declaration — English the default, Portuguese
// behind it — at the revision a page was drawn at. A stale form that brought back the
// pair the row already holds would be a retry of a write that landed, and the module
// answers that with no write and no event at any revision (the shared suite case
// "the declaration the tenant already holds changes nothing and says nothing" decides
// it against both implementations). The form below is the case the revision floor
// exists for: the row holds something else, so the write would lose it.
func postOtherLanguages(t *testing.T, cfg config.Config, client *http.Client, revision string) (int, string) {
	t.Helper()
	return postLanguagesForm(t, cfg, client, url.Values{
		"default": {"en"}, "supported": {"pt-PT"}, "localeRevision": {revision},
	})
}

// postLanguagesForm is the request both forms above make, with redirects left for the
// case to read: where a save lands is part of the answer under test.
func postLanguagesForm(t *testing.T, cfg config.Config, client *http.Client, form url.Values) (int, string) {
	t.Helper()
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequest(http.MethodPost, "http://"+cfg.Server.Addr+"/app/tenant/languages",
		strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = acmeHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "http://"+acmeHost+"/app/tenant/languages")
	res, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("POST /app/tenant/languages: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read the answer: %v", err)
	}
	return res.StatusCode, string(body)
}

// TestAnAdministratorSetsTheTenantsLanguagesAndTheTrailHoldsIt: the declaration, the
// event and the audit record all appear, and a stale form appears nowhere.
func TestAnAdministratorSetsTheTenantsLanguagesAndTheTrailHoldsIt(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	acme := installationTenantID(t, cfg, admin, "acme")
	// Start from a tenant served in English alone, so the Portuguese the case asks
	// for is a change and not the fixture's own starting state.
	declareLocale(t, cfg, admin, acme, `{"default":"en","supported":[]}`)

	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	revision := func() string {
		var at int64
		if err := owner.QueryRowContext(t.Context(),
			`SELECT locale_revision FROM tenants WHERE id = $1`, acme).Scan(&at); err != nil {
			t.Fatalf("read the tenant's locale revision: %v", err)
		}
		return fmt.Sprint(at)
	}
	before := revision()

	// The page itself carries the revision it was drawn against — the form cannot
	// expect a number it never showed, and this is the assertion that it does.
	_, _, _, page := r7navigate(t, cfg, admin, acmeHost, "/app/tenant/languages", "en")
	shown := regexp.MustCompile(`name="localeRevision"[^>]*value="(\d+)"`).FindStringSubmatch(page)
	if shown == nil {
		shown = regexp.MustCompile(`value="(\d+)"[^>]*name="localeRevision"`).FindStringSubmatch(page)
	}
	if shown == nil || shown[1] != before {
		t.Fatalf("the languages page carries localeRevision %v, want the tenant's %s:\n%s", shown, before, firstLineOf(page))
	}

	if code, body := postLanguages(t, cfg, admin, before); code != http.StatusSeeOther {
		t.Fatalf("POST /app/tenant/languages = %d, want 303: %s", code, firstLineOf(body))
	}

	// The row.
	var (
		defaultTag string
		others     []string
	)
	eventually(t, "the tenant's declaration is written", func() bool {
		if err := owner.QueryRowContext(t.Context(),
			`SELECT default_locale FROM tenants WHERE id = $1`, acme).Scan(&defaultTag); err != nil {
			return false
		}
		rows, err := owner.QueryContext(t.Context(),
			`SELECT locale FROM tenant_locales WHERE tenant_id = $1 AND locale <> $2 ORDER BY locale`, acme, defaultTag)
		if err != nil {
			return false
		}
		defer rows.Close()
		others = nil
		for rows.Next() {
			var tag string
			if err := rows.Scan(&tag); err != nil {
				t.Fatal(err)
			}
			others = append(others, tag)
		}
		return defaultTag == "pt-PT" && len(others) == 1 && others[0] == "en"
	})
	if defaultTag != "pt-PT" || len(others) != 1 || others[0] != "en" {
		t.Fatalf("acme is served in %q plus %v, want pt-PT plus [en]", defaultTag, others)
	}

	// The event and the audit record, with the actor who asked.
	var events, audits int
	var actor string
	eventually(t, "tenant.locale_set reaches the outbox and the audit trail", func() bool {
		if err := owner.QueryRowContext(t.Context(),
			`SELECT count(*) FROM platformkit_outbox WHERE name = 'tenant.locale_set'`).Scan(&events); err != nil {
			return false
		}
		if events == 0 {
			return false
		}
		err := owner.QueryRowContext(t.Context(),
			`SELECT count(*), coalesce(max(actor::text), '') FROM audit_events WHERE name = 'tenant.locale_set'`).
			Scan(&audits, &actor)
		return err == nil && audits > 0 && actor != ""
	})

	// The revision moved, which is what the next stale form is refused against.
	after := revision()
	if after == before {
		t.Fatalf("locale_revision stayed at %s across a changed declaration", before)
	}

	// The refusal is compared against a trail that has stopped moving: the relay
	// feeds audit_events once a second, so the readings above can still have been
	// arriving when they were taken. Settling first is what makes the comparison a
	// statement about the refused write rather than about the relay's tick.
	events, audits = settledLanguageTrail(t, owner)

	// A stale write is refused and leaves nothing behind: not the row, not the
	// count of events, not a second audit record. The old revision the person's
	// other tab read is the evidence, and it stays the evidence. The form brings the
	// other declaration, deliberately: a stale form carrying the pair the row already
	// holds changes nothing and is answered as a retry, further down, and neither
	// case is the one the floor below is for. What the floor is for is this form —
	// the row holds something else, so letting it through would lose a declaration.
	// A page refuses by answering itself, with the sentence above the form — the
	// same choice the roles screen makes, so nobody loses the set they were choosing.
	// What is asserted is that the answer is not a redirect (the write did not
	// happen), that it names the revision, and that nothing below it moved.
	code, body := postOtherLanguages(t, cfg, admin, before)
	if code == http.StatusSeeOther {
		t.Errorf("a stale languages form redirected as if it had written")
	}
	if !strings.Contains(body, "revision") {
		t.Errorf("the stale form's answer names no revision: %s", firstLineOf(body))
	}
	var eventsAgain, auditsAgain int
	if err := owner.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE name = 'tenant.locale_set'`).Scan(&eventsAgain); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(),
		`SELECT count(*) FROM audit_events WHERE name = 'tenant.locale_set'`).Scan(&auditsAgain); err != nil {
		t.Fatal(err)
	}
	if eventsAgain != events || auditsAgain != audits {
		t.Errorf("the refused write left %d events and %d audit rows where the accepted one left %d and %d",
			eventsAgain, auditsAgain, events, audits)
	}
	if still := revision(); still != after {
		t.Errorf("the refused write moved locale_revision from %s to %s", after, still)
	}

	// The other half of the same floor, at the same stale revision: a form carrying
	// the pair the row already holds — a browser retry of a save that landed, an
	// administrator who pressed save twice. Nothing would change if it were applied,
	// so nothing is applied: the page answers the way a save does and the row, the
	// revision and the trail stay where the accepted write left them. This is the
	// composition's share of the suite case "the declaration the tenant already
	// holds changes nothing and says nothing".
	if code, body := postLanguages(t, cfg, admin, before); code != http.StatusSeeOther {
		t.Errorf("a stale form carrying the declaration the tenant holds = %d, want 303 like any saved form: %s",
			code, firstLineOf(body))
	}
	if still := revision(); still != after {
		t.Errorf("a form carrying the declaration already held moved locale_revision from %s to %s", after, still)
	}
	var retriedEvents, retriedAudits int
	if err := owner.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE name = 'tenant.locale_set'`).Scan(&retriedEvents); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(),
		`SELECT count(*) FROM audit_events WHERE name = 'tenant.locale_set'`).Scan(&retriedAudits); err != nil {
		t.Fatal(err)
	}
	if retriedEvents != eventsAgain || retriedAudits != auditsAgain {
		t.Errorf("the retry left %d events and %d audit rows, where the refusal above left %d and %d",
			retriedEvents, retriedAudits, eventsAgain, auditsAgain)
	}
}

// settledLanguageTrail answers with the tenant.locale_set counts once two readings a
// second apart agree, so a caller can compare a refused write against a settled pair
// instead of a moment. The relay ticks once a second, which is why the beat is longer
// than the tick: a shorter one can straddle a delivery and call it settled.
func settledLanguageTrail(t *testing.T, owner *sql.DB) (int, int) {
	t.Helper()
	var lastEvents, lastAudits int
	for range 20 {
		var events, audits int
		if err := owner.QueryRowContext(t.Context(),
			`SELECT count(*) FROM platformkit_outbox WHERE name = 'tenant.locale_set'`).Scan(&events); err != nil {
			t.Fatalf("count the outbox: %v", err)
		}
		if err := owner.QueryRowContext(t.Context(),
			`SELECT count(*) FROM audit_events WHERE name = 'tenant.locale_set'`).Scan(&audits); err != nil {
			t.Fatalf("count the audit trail: %v", err)
		}
		if events > 0 && audits > 0 && events == lastEvents && audits == lastAudits {
			return events, audits
		}
		lastEvents, lastAudits = events, audits
		time.Sleep(1100 * time.Millisecond)
	}
	t.Fatalf("the tenant.locale_set trail never stopped moving")
	return 0, 0
}
