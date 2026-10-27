package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/providers/gomail/gomailtest"
	"github.com/septagon-oss/platformkit/pkit"
)

// The public doors that answer a person as though a mail were on its way: sign-up,
// another verification link, and the forgotten-password request. Each promises an
// email in its acknowledgment, so each is refused where nothing can deliver one.
var accountEmailDoors = []struct{ path, body string }{
	{"/api/v1/public/auth/register", `{"email":"nobody@acme.localhost","displayName":"Nobody","password":"a password of their own","confirmation":"a password of their own","termsAccepted":true}`},
	{"/api/v1/public/auth/resend-verification", `{"email":"nobody@acme.localhost"}`},
	{"/api/v1/public/auth/password/forgot", `{"email":"nobody@acme.localhost"}`},
}

const noMailSentence = "account email delivery is unavailable"

// startReference boots the reference composition the way every case here boots it:
// installed, composed, serving every role, with an in-memory transport.
func startReference(t *testing.T, path string, cfg config.Config) *composition {
	t.Helper()
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	return &c
}

// rows reads the tables an account-email request could write, as the schema owner:
// this is the count a refused request must not move, and the owner sees the whole
// namespace rather than one tenant's share of it.
func rows(t *testing.T, owner *sql.DB, tables ...string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, table := range tables {
		var n int64
		if err := owner.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		out[table] = n
	}
	return out
}

var writtenTables = []string{"users", "password_tokens", "verification_tokens", "direct_mail_deliveries"}

// authEvents counts this module's own outbox rows. The kernel writes an operation
// outcome for every refused mutation, correct and expected, so the count that has
// to stay still across a refused account email is the one filtered to auth's names.
func authEvents(t *testing.T, owner *sql.DB) int64 {
	t.Helper()
	var n int64
	if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox WHERE name LIKE 'auth.%'`).Scan(&n); err != nil {
		t.Fatalf("count auth's outbox rows: %v", err)
	}
	return n
}

// TestAMailLessCompositionRefusesEveryAccountEmailDoor is the reference composition
// as it stands with no mail host — the deployment this repository's own
// testdata/composition.yaml describes, and the one a development machine without an
// SMTP server runs. It holds a sender, because every composition does, and that
// sender records rather than dials. Each door answers the refusal, and the
// installation writes nothing: no account, no token, no delivery record, and no
// outbox row for a worker to make a promise out of.
func TestAMailLessCompositionRefusesEveryAccountEmailDoor(t *testing.T) {
	path, cfg := configure(t)
	c := startReference(t, path, cfg)
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("this composition mails through %T; the case is about the recording carrier", c.mail)
	}
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	before := rows(t, owner, writtenTables...)
	_, port, err := net.SplitHostPort(cfg.Server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	served := acmeHost + ":" + port
	for _, door := range accountEmailDoors {
		code, body := do(t, cfg, nil, http.MethodPost, served, door.path, door.body)
		if code != http.StatusServiceUnavailable {
			t.Errorf("POST %s = %d %s, want the unavailable refusal", door.path, code, body)
		}
		if !json.Valid([]byte(body)) || !strings.Contains(body, noMailSentence) {
			t.Errorf("POST %s said %q, want the deployment's own unavailable sentence", door.path, body)
		}
	}
	after := rows(t, owner, writtenTables...)
	for table, was := range before {
		if after[table] != was {
			t.Errorf("%s holds %d rows where it held %d before a refused request, and the outbox now holds %s",
				table, after[table], was, outboxNames(t, owner))
		}
	}
	if n := authEvents(t, owner); n != 0 {
		t.Errorf("a refused account email published %d of auth's events, want none: %s", n, outboxNames(t, owner))
	}
	if n := len(box.Sent()); n != 0 {
		t.Errorf("a refused installation handed %d messages to its carrier, want none", n)
	}
	// The account the refused sign-up would have made is not there to sign in to:
	// the refusal is the whole of what that call did.
	code, _ := do(t, cfg, nil, http.MethodPost, served, "/api/v1/public/auth/login",
		`{"email":"nobody@acme.localhost","password":"a password of their own"}`)
	if code != http.StatusNotFound && code != http.StatusUnauthorized {
		t.Errorf("signing in as the address that was refused answered %d, want the answer an address nobody has gets", code)
	}
}

// TestTheConnectionScreenAndTheAPIAnswerAvailabilityAlike is the one reading of
// availability, asked of two surfaces of the same installation. The connection
// document draws or hides the forgotten-password control, and the API door is the
// control's own request: a deployment that reads one answer on the page and the
// opposite at the door sends a person to a form the server refuses.
func TestTheConnectionScreenAndTheAPIAnswerAvailabilityAlike(t *testing.T) {
	for _, leg := range []struct {
		name       string
		mailHost   bool
		wantScreen bool
		wantStatus int
	}{
		{"no mail host", false, false, http.StatusServiceUnavailable},
		{"a mail host", true, true, http.StatusOK},
	} {
		t.Run(leg.name, func(t *testing.T) {
			var (
				path  string
				cfg   config.Config
				relay *gomailtest.Relay
			)
			if leg.mailHost {
				path, cfg, relay = configureMailing(t)
			} else {
				path, cfg = configure(t)
			}
			startReference(t, path, cfg)
			_, port, err := net.SplitHostPort(cfg.Server.Addr)
			if err != nil {
				t.Fatal(err)
			}
			served := acmeHost + ":" + port
			code, body := do(t, cfg, nil, http.MethodGet, served, connectionPath, "")
			if code != http.StatusOK {
				t.Fatalf("GET %s = %d %s", connectionPath, code, body)
			}
			document := connectionBody(t, body)
			recovery, _ := document["recovery"].(map[string]any)
			if recovery["available"] != leg.wantScreen {
				t.Errorf("the connection screen says recovery is %v, want %v", recovery["available"], leg.wantScreen)
			}
			code, body = do(t, cfg, nil, http.MethodPost, served, "/api/v1/public/auth/password/forgot", `{"email":"`+adminEmail+`"}`)
			if code != leg.wantStatus {
				t.Errorf("the forgotten-password door answered %d %s, want %d — the answer the page drew", code, body, leg.wantStatus)
			}
			if relay != nil && waitForMatter(relay, 20*time.Second) == 0 {
				t.Errorf("the connection screen offered the door and the relay saw no mail for it")
			}
		})
	}
}

// TestAMailHostCompositionAcceptsWhatItCanMail is the other half of the decision:
// the same installation with a mail host named, which is how a development instance
// and every production one signs people up. The doors accept, the account and the
// request commit, and the message the worker sent is sitting in the relay — which is
// what "nothing changes when mail can leave" means in a test.
func TestAMailHostCompositionAcceptsWhatItCanMail(t *testing.T) {
	path, cfg, relay := configureMailing(t)
	c := startReference(t, path, cfg)
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	before := rows(t, owner, writtenTables...)
	_, port, err := net.SplitHostPort(cfg.Server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	served := acmeHost + ":" + port
	const email = "mails.fine@acme.localhost"
	signup, err := json.Marshal(map[string]any{
		"email": email, "displayName": "Mails Fine", "password": "a password of their own",
		"confirmation": "a password of their own", "termsAccepted": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := do(t, cfg, nil, http.MethodPost, served, "/api/v1/public/auth/register", string(signup)); code != http.StatusAccepted {
		t.Fatalf("signup = %d %s, want the acceptance this installation can honour", code, body)
	}
	if code, body := do(t, cfg, nil, http.MethodPost, served, "/api/v1/public/auth/resend-verification", `{"email":"`+email+`"}`); code != http.StatusAccepted {
		t.Fatalf("resend = %d %s", code, body)
	}
	confirmation := waitForLink(t, relay, email)
	if confirmation == "" {
		t.Fatalf("no confirmation link reached the relay; it kept %d messages and the ledger says %s",
			len(relay.Sent()), mailRecords(t, owner))
	}
	if !strings.HasPrefix(confirmation, "http://"+served+contracts.VerifyEmailPath+"?token=") {
		t.Errorf("the confirmation link leads elsewhere: %s", confirmation)
	}
	after := rows(t, owner, writtenTables...)
	if after["users"] != before["users"]+1 {
		t.Errorf("the account count moved from %d to %d, want the one person who signed up", before["users"], after["users"])
	}
	{
		rs, err := owner.Query("SELECT name, count(*) FROM platformkit_outbox GROUP BY name ORDER BY 1")
		if err == nil {
			for rs.Next() {
				var n string
				var c int64
				_ = rs.Scan(&n, &c)
				t.Logf("outbox %s = %d", n, c)
			}
			rs.Close()
		}
		rs2, err2 := owner.Query("SELECT kind, outcome, reason FROM direct_mail_deliveries")
		if err2 == nil {
			for rs2.Next() {
				var k, o, r string
				_ = rs2.Scan(&k, &o, &r)
				t.Logf("delivery %s %s %s", k, o, r)
			}
			rs2.Close()
		}
		t.Logf("relay kept %d messages", len(relay.Sent()))
	}
	if after["users"] != before["users"]+1 {
		t.Errorf("the account count stayed at %d where a person signed up", after["users"])
	}
	if code, body := do(t, cfg, nil, http.MethodPost, served, "/api/v1/public/auth/password/forgot", `{"email":"`+email+`"}`); code != http.StatusOK && code != http.StatusAccepted {
		t.Errorf("the forgotten-password door answered %d %s where a mail host is named", code, body)
	}
	if _, ok := pkit.Value[notificationcontracts.MailDelivery](c.planned); !ok {
		t.Error("the composition put no delivery answer in the plan")
	}
}

// outboxNames is every event name in the schema and how many of it there are.
func outboxNames(t *testing.T, owner *sql.DB) string {
	t.Helper()
	rs, err := owner.Query("SELECT name, count(*) FROM platformkit_outbox GROUP BY name ORDER BY 1")
	if err != nil {
		return err.Error()
	}
	defer rs.Close()
	var out strings.Builder
	for rs.Next() {
		var name string
		var n int64
		if err := rs.Scan(&name, &n); err != nil {
			return err.Error()
		}
		fmt.Fprintf(&out, "%s=%d ", name, n)
	}
	return out.String()
}

// mailRecords is what this installation's ledger says about its mails.
func mailRecords(t *testing.T, owner *sql.DB) string {
	t.Helper()
	rs, err := owner.Query("SELECT kind, outcome, reason FROM direct_mail_deliveries")
	if err != nil {
		return err.Error()
	}
	defer rs.Close()
	var out strings.Builder
	for rs.Next() {
		var kind, outcome, reason string
		if err := rs.Scan(&kind, &outcome, &reason); err != nil {
			return err.Error()
		}
		fmt.Fprintf(&out, "%s/%s(%s) ", kind, outcome, reason)
	}
	return out.String()
}

// linkIn is the link in a message body, or the empty string.
func linkIn(body string) string {
	return regexp.MustCompile(`https?://\S+token=\S+`).FindString(body)
}

// waitForMatter is how long the relay stayed empty.
func waitForMatter(relay *gomailtest.Relay, wait time.Duration) int {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if n := len(relay.Sent()); n > 0 {
			return n
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0
}

// waitForLink is the link the relay received for one address, or the empty string
// once it has had a moment to be dialled.
func waitForLink(t *testing.T, relay *gomailtest.Relay, email string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, sent := range relay.Sent() {
			if sent.To == email {
				if link := linkIn(sent.Body); link != "" {
					return link
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ""
}
