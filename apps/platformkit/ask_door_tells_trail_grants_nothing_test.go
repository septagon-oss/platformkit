package main

// The pin over the ask's
// other door: the kernel's own JSON one, POST /api/v1/app/access-requests.
//
// Every ask case on this branch asks the browser door — apps/platformkit's journey,
// ui/page's form, kit/httpx's command behind a mounted probe — and IMPLEMENT.md
// names the gap itself (its deferred item 3): "the command and its guard are covered
// by kit/httpx/ask_test.go behind a mounted signed-in door; the composed address is
// not." The address matters more than the gap looks: it is the door a native shell
// reads from the workspace catalogue and calls, so a person on a phone has only it,
// and nothing on this branch asked the composed application whether that person is
// refused for the right reason when anonymous, told the same notice when they are
// not, and still refused afterwards.
//
// What is asserted here is parity with the page, not a second copy of the page's
// journey: the guard, the writes (one notice into the tenant's bell, one trail row
// with the notices it wrote), the verdict at the address that refused, and the fact
// that the two doors are one command — an ask through each leaves two of each, and no
// third of either. The refusal's four parts are the page's own claim,
// TestARefusedPersonSeesWhatIsMissingWhoCanGrantItAsksAndIsGranted walks them, and
// nothing below asks a sentence.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// jsonAskPath is the kernel's door on the workspace surface. The kernel names the
// address (kit/app/access.go mountAccessRequest); this file only asks it.
const jsonAskPath = "/api/v1/app/access-requests"

func TestTheJSONAskDoorTellsTheSameTrailAndGrantsNothing(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	const asked = `{"permission":"task:read","path":"/app/task/tasks"}`
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	asker := "json.ask@acme.test"
	provisionAs(t, cfg, c, acmeTenant(t, cfg), asker, "member")
	member := signIn(t, cfg, acmeHost, asker, adminPass)
	memberID := userIDOf(t, cfg, asker)

	// 1. Anonymous: refused at the guard, for the reason the guard gives, and the
	// ask wrote nothing. The refusal is the answer, not a half-write with it.
	code, body := do(t, cfg, nil, http.MethodPost, acmeHost, jsonAskPath, asked)
	if code != http.StatusForbidden || !strings.Contains(body, "AUTH_ANONYMOUS") {
		t.Errorf("POST %s anonymously = %d %s, want 403 naming AUTH_ANONYMOUS", jsonAskPath, code, body)
	}
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, noticePath, ""); code != http.StatusOK ||
		strings.Contains(body, "Access requested") {
		t.Fatalf("an anonymous ask wrote into the administrator's bell: %d %s", code, body)
	}

	// 2. The refused person, at the JSON door.
	code, body = do(t, cfg, member, http.MethodPost, acmeHost, jsonAskPath, asked)
	if code != http.StatusAccepted {
		t.Fatalf("POST %s as the refused person = %d %s, want 202", jsonAskPath, code, body)
	}
	eventually(t, "the ask through the JSON door reaches acme's administrator's bell, once", func() bool {
		code, body = do(t, cfg, admin, http.MethodGet, acmeHost, noticePath, "")
		return code == http.StatusOK && strings.Count(body, "Access requested") == 1
	})
	if !strings.Contains(body, memberID.String()) || !strings.Contains(body, "task:read") {
		t.Errorf("the notice names neither the asker nor the grant: %s", body)
	}
	if !strings.Contains(body, "/app/user/users/"+memberID.String()) {
		t.Errorf("the notice carries no deep link to the person's own screen: %s", body)
	}

	// 3. The trail says what the bell says: one ask, one notice written. The row
	// reaches the trail through the outbox, so it is waited for the way every other
	// trail read on this branch waits for it.
	askedRows, notified := 0, ""
	eventually(t, "security.access_requested in the trail", func() bool {
		askedRows, notified = askTrail(t, cfg)
		return askedRows == 1
	})
	if notified != "1" {
		t.Errorf("security.access_requested recorded notified=%s after one ask, want \"1\"", notified)
	}

	// 4. An ask is not a grant: the address that refused answers exactly as it did.
	if status, _, page := askNavigate(t, cfg, member, acmeHost, askDeniedScreen); status != http.StatusForbidden {
		t.Errorf("GET %s after an ask = %d, want the same 403: %s", askDeniedScreen, status, firstLineOf(page))
	}

	// 5. A key nothing declares names no grant, and writes nothing.
	if code, body = do(t, cfg, member, http.MethodPost, acmeHost, jsonAskPath,
		`{"permission":"widget:read","path":"/app/widget/widgets"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("asking for a permission no module declares = %d %s, want 422", code, body)
	}
	if askedRows, _ = askTrail(t, cfg); askedRows != 1 {
		t.Errorf("an ask for an undeclared permission reached the trail: %d rows", askedRows)
	}

	// 6. Two doors, one command: the browser's form lands beside the JSON ask, in
	// the same bell and the same trail, with the same count of notices.
	if code, location, posted := askForm(t, cfg, member, acmeHost, pinnedAsk,
		"permission=task%3Aread&path=%2Fapp%2Ftask%2Ftasks"); code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d %s, want the 303", pinnedAsk, code, posted)
	} else if want := "/app/access-request/sent"; location != want {
		t.Errorf("the browser's ask lands at %q, want %q", location, want)
	}
	eventually(t, "both doors' asks stand in the bell", func() bool {
		code, body = do(t, cfg, admin, http.MethodGet, acmeHost, noticePath, "")
		return code == http.StatusOK && strings.Count(body, "Access requested") == 2
	})
	eventually(t, "both doors' asks in the trail", func() bool {
		askedRows, notified = askTrail(t, cfg)
		return askedRows == 2
	})
	if notified != "1" {
		t.Errorf("the newest ask of the two doors recorded notified=%s, want the one notice each wrote", notified)
	}
}

// askTrail reads the ask's own rows the way an operator would: how many there are,
// and how many notices the newest one claims to have written.
func askTrail(t *testing.T, cfg config.Config) (int, string) {
	t.Helper()
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var rows int
	var notified string
	err := owner.QueryRowContext(t.Context(),
		`SELECT count(*), coalesce(max(payload->>'notified'), '')
		   FROM audit_events WHERE name = 'security.access_requested'`).Scan(&rows, &notified)
	if err != nil {
		t.Fatalf("read the ask trail: %v", err)
	}
	return rows, notified
}
