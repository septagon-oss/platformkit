package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/notification"
)

func TestInvitationHTTPReplayAndRevocationReturnCommittedState(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	const endpoint = "/api/v1/user/invitations"
	const body = `{"email":"lifecycle@acme.localhost","displayName":"Lifecycle","roles":["member"]}`
	code, first, _ := invitationHTTP(t, cfg, admin, endpoint, body, "invitation.create.lost-response")
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, first)
	}
	code, replay, headers := invitationHTTP(t, cfg, admin, endpoint, body, "invitation.create.lost-response")
	if code != http.StatusCreated || first != replay {
		t.Fatalf("replay: %d %s, original %s", code, replay, first)
	}
	id := field(t, first, "id")
	reading := "/api/v1/auth/invitations/" + id
	if headers.Get("Location") != reading {
		t.Fatalf("recovery Location = %q", headers.Get("Location"))
	}
	code, conflict, _ := invitationHTTP(t, cfg, admin, endpoint, strings.ReplaceAll(body, "Lifecycle", "Changed"), "invitation.create.lost-response")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("changed replay: %d %s", code, conflict)
	}
	code, refusal := do(t, cfg, admin, http.MethodPost, acmeHost, endpoint, `{"email":"bad-role@acme.localhost","roles":["undefined-role"]}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid role: %d %s", code, refusal)
	}
	code, refusal = do(t, cfg, admin, http.MethodPost, acmeHost, usersPath+"/"+id+"/roles", `{"roles":["admin"]}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("revisionless role edit: %d %s", code, refusal)
	}
	code, cooling, h := invitationHTTP(t, cfg, admin, endpoint+"/"+id+"/resend", `{"expectedRevision":1}`, "resend.cooling")
	if code != http.StatusTooManyRequests || h.Get("Retry-After") == "" {
		t.Fatalf("cooldown: %d %s %v", code, cooling, h)
	}
	box := c.mail.(*notification.Mailbox)
	eventually(t, "invitation mail handed to the mailbox", func() bool {
		return len(mailedTo(box, "lifecycle@acme.localhost")) > 0
	})
	link := mailedTo(box, "lifecycle@acme.localhost")[0]
	assertInvitationHTTPReading(t, cfg, admin, reading, "pending", "unobserved")
	assertResendHTTPReplay(t, cfg, admin, id, tokenIn(t, link))
	// The handoff is the worker's, later than the command that committed it, and a
	// withdrawn invitation is refused a send that has not run yet: the count waits here.
	eventually(t, "the replacement handoff", func() bool {
		return len(mailedTo(box, "lifecycle@acme.localhost")) >= 2
	})
	assertLegacyDeactivationRevision(t, cfg, admin)
	code, revoked, _ := invitationHTTP(t, cfg, admin, endpoint+"/"+id+"/revoke", `{"expectedRevision":2}`, "invitation.revoke.lost-response")
	if code != http.StatusOK {
		t.Fatalf("revoke: %d %s", code, revoked)
	}
	code, again, _ := invitationHTTP(t, cfg, admin, endpoint+"/"+id+"/revoke", `{"expectedRevision":2}`, "invitation.revoke.lost-response")
	if code != http.StatusOK || again != revoked {
		t.Fatalf("revoke replay: %d %s", code, again)
	}
	code, refusal = do(t, cfg, admin, http.MethodPost, acmeHost, endpoint+"/"+id+"/revoke", `{"expectedRevision":2}`)
	if code != http.StatusConflict {
		t.Fatalf("stale revoke: %d %s", code, refusal)
	}
	assertInvitationHTTPReading(t, cfg, admin, reading, "revoked", "unobserved")
	code, refusal = do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/auth/password/reset", `{"token":"`+tokenIn(t, link)+`","new":"a sufficiently strong password"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("revoked acceptance: %d %s", code, refusal)
	}
	// Two messages and no third: the replayed create handed one over, the resend
	// handed the replacement, and the withdrawal hands nothing over.
	if got := mailedTo(box, "lifecycle@acme.localhost"); len(got) != 2 {
		t.Fatalf("replayed invitation sent %d messages", len(got))
	}
}

// mailedTo returns the body of every invitation message the mailbox holds for one
// address, oldest first. A handoff is a worker's work and not a command's, so the
// number of them is an asynchronous observable a case has to wait for.
func mailedTo(box *notification.Mailbox, to string) (bodies []string) {
	for _, m := range box.Sent() {
		if m.To == to {
			bodies = append(bodies, m.Body)
		}
	}
	return
}
func invitationHTTP(t *testing.T, cfg config.Config, client *http.Client, path, body, key string) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+cfg.Server.Addr+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.IdempotencyKeyHeader, key)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	out, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(out), response.Header
}
func assertInvitationHTTPReading(t *testing.T, cfg config.Config, client *http.Client, path, state, observation string) {
	t.Helper()
	eventually(t, "authoritative invitation reading", func() bool {
		code, body := do(t, cfg, client, http.MethodGet, acmeHost, path, "")
		if code != http.StatusOK {
			return false
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(body), &row); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"deliveryId", "generation", "passwordHash", "token", "tokenHash"} {
			if _, ok := row[key]; ok {
				t.Fatalf("reading exposes %s", key)
			}
		}
		return row["state"] == state && row["observation"] == observation
	})
}

func assertResendHTTPReplay(t *testing.T, cfg config.Config, admin *http.Client, id, oldToken string) {
	t.Helper()
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	if _, err := owner.Exec(`UPDATE users SET invitation_requested_at=now()-interval '5 minutes',
 invitation_expires_at=now()+interval '55 minutes' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/user/invitations/" + id + "/resend"
	first, body, _ := invitationHTTP(t, cfg, admin, path, `{"expectedRevision":1}`, "invitation.resend.lost-response")
	if first != http.StatusAccepted {
		t.Fatalf("resend: %d %s", first, body)
	}
	again, replay, _ := invitationHTTP(t, cfg, admin, path, `{"expectedRevision":1}`, "invitation.resend.lost-response")
	if again != http.StatusAccepted || body != replay {
		t.Fatalf("resend replay: %d %s", again, replay)
	}
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, path, `{"expectedRevision":1}`); code != http.StatusConflict {
		t.Fatalf("stale resend: %d %s", code, body)
	}
	if code, body := do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/auth/password/reset", `{"token":"`+oldToken+`","new":"a sufficiently strong password"}`); code != http.StatusUnauthorized {
		t.Fatalf("obsolete link: %d %s", code, body)
	}
	assertInvitationHTTPReading(t, cfg, admin, "/api/v1/auth/invitations/"+id, "pending", "unobserved")
}

func assertLegacyDeactivationRevision(t *testing.T, cfg config.Config, admin *http.Client) {
	t.Helper()
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/user/invitations", `{"email":"deactivate-invitation@acme.localhost"}`)
	if code != http.StatusCreated {
		t.Fatalf("deactivation fixture: %d %s", code, body)
	}
	id := field(t, body, "id")
	path := usersPath + "/" + id + "/deactivate"
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, path, `{}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("revisionless deactivation: %d %s", code, body)
	}
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, path+"?expectedRevision=1", `{}`); code != http.StatusOK {
		t.Fatalf("versioned deactivation: %d %s", code, body)
	}
	if code, body := do(t, cfg, admin, http.MethodGet, acmeHost, "/api/v1/auth/invitations/"+id, ""); code != http.StatusOK || !strings.Contains(body, `"state":"revoked"`) {
		t.Fatalf("deactivation reading: %d %s", code, body)
	}
}
