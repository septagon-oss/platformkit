package main

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// TestQuotingTheInvitationRevisionEditsASettledMember checks the role edit of a
// person whose invitation is closed. The invitation revision is the only write
// count any reading of this lifecycle publishes, and a settled person has no open
// invitation for it to answer, so quoting it has to edit the person rather than
// refuse a conflict the row is not in: the ordinary write stays the ordinary write.
func TestQuotingTheInvitationRevisionEditsASettledMember(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the reference application mails through %T", c.mail)
	}
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	const email = "quoting.member@acme.localhost"
	code, invited := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/user/invitations",
		`{"email":"`+email+`","displayName":"Quoting Member","roles":["member"]}`)
	if code != http.StatusCreated {
		t.Fatalf("invite: %d %s", code, invited)
	}
	id := field(t, invited, "id")
	const chosen = "a password this person chose themselves"
	person := &http.Client{Jar: mustJar(t)}
	redeemMailedLink(t, cfg, person, acmeHost, "/api/v1/auth/password/reset",
		`{"token":"`+tokenIn(t, mailedLink(t, box, email))+`","new":"`+chosen+`"}`)

	reading := usersPath + "/" + id
	code, member := do(t, cfg, admin, http.MethodGet, acmeHost, reading, "")
	if code != http.StatusOK || !strings.Contains(member, `"status":"active"`) {
		t.Fatalf("the person who accepted is not an active member: %d %s", code, member)
	}
	quoted := quotedInvitationRevision(t, cfg, admin, id)
	code, refusal := do(t, cfg, admin, http.MethodPost, acmeHost, reading+"/roles",
		`{"roles":["member","observer"],"expectedRevision":`+strconv.FormatInt(quoted, 10)+`}`)
	if code != http.StatusOK {
		t.Fatalf("re-roled while quoting invitation revision %d answered %d %s; the person is a settled member with no open invitation that revision could answer",
			quoted, code, refusal)
	}
	code, edited := do(t, cfg, admin, http.MethodGet, acmeHost, reading, "")
	if code != http.StatusOK || !strings.Contains(edited, `"observer"`) {
		t.Fatalf("the role edit did not reach the row: %d %s (was %s)", code, edited, member)
	}
}

// TestAWaitingInvitationRoleEditQuotesItsOwnRevision keeps the other half of the
// same predicate: a person whose invitation is still open is re-roled through the
// invitation command, at the invitation's own revision, so a settled person's
// answer above cannot be reached by dropping the revision machinery for everybody.
func TestAWaitingInvitationRoleEditQuotesItsOwnRevision(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, invited := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/user/invitations",
		`{"email":"still.waiting@acme.localhost","displayName":"Still Waiting","roles":["member"]}`)
	if code != http.StatusCreated {
		t.Fatalf("invite: %d %s", code, invited)
	}
	id := field(t, invited, "id")
	reading := usersPath + "/" + id
	if code, refusal := do(t, cfg, admin, http.MethodPost, acmeHost, reading+"/roles",
		`{"roles":["member","observer"]}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("an open invitation re-roled without a revision answered %d %s", code, refusal)
	}
	if code, refusal := do(t, cfg, admin, http.MethodPost, acmeHost, reading+"/roles",
		`{"roles":["member","observer"],"expectedRevision":1}`); code != http.StatusOK {
		t.Fatalf("an open invitation re-roled at its own revision answered %d %s", code, refusal)
	}
	if code, edited := do(t, cfg, admin, http.MethodGet, acmeHost, reading, ""); code != http.StatusOK ||
		!strings.Contains(edited, `"observer"`) {
		t.Fatalf("the invitation role edit did not reach the row: %d %s", code, edited)
	}
}

// quotedInvitationRevision waits for the invitation of one person to read as
// accepted and returns the revision that reading publishes — the number a caller
// is shown, and the only write count this lifecycle shows anybody.
func quotedInvitationRevision(t *testing.T, cfg config.Config, admin *http.Client, id string) int64 {
	t.Helper()
	var revision int64
	eventually(t, "the accepted invitation reading", func() bool {
		code, body := do(t, cfg, admin, http.MethodGet, acmeHost, "/api/v1/auth/invitations/"+id, "")
		if code != http.StatusOK || !strings.Contains(body, `"state":"accepted"`) {
			return false
		}
		var row struct {
			Revision int64 `json:"revision"`
		}
		if err := json.Unmarshal([]byte(body), &row); err != nil {
			t.Fatalf("read the invitation revision from %s: %v", body, err)
		}
		if row.Revision == 0 {
			t.Fatalf("no invitation revision in %s", body)
		}
		revision = row.Revision
		return true
	})
	return revision
}

// mailedLink waits for the mailbox to hold the first message addressed to one
// address and returns its body: a handoff is the worker's work, so what the
// mailbox holds is an asynchronous observable a case has to wait for.
func mailedLink(t *testing.T, box *notification.Mailbox, to string) string {
	t.Helper()
	var body string
	eventually(t, "the invitation mail for "+to, func() bool {
		for _, sent := range box.Sent() {
			if sent.To == to {
				body = sent.Body
				return true
			}
		}
		return false
	})
	return body
}

func mustJar(t *testing.T) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}
