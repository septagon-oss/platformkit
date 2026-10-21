package main

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/notification"
)

func TestProposalNotificationsAreNotDuplicatedByEventReplay(t *testing.T) {
	cfg, c, opts, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("mailer = %T, want the existing fixture mailbox", c.mail)
	}
	reviewer := decider(t, cfg, admin, box, "notice-reviewer")
	id := newTask(t, cfg, admin, "Service the pump", "normal")
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+id+`","diff":{"priority":"high"},"summary":"Bring maintenance forward"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("propose = %d %s", code, body)
	}
	pid := field(t, body, "id")
	if code, body = do(t, cfg, reviewer, http.MethodPost, acmeHost, proposalsPath+"/"+pid+"/review",
		`{"verdict":"approved","expectedRevision":1}`); code != http.StatusOK {
		t.Fatalf("review = %d %s", code, body)
	}
	if code, body = do(t, cfg, reviewer, http.MethodPost, acmeHost, proposalsPath+"/"+pid+"/apply",
		`{"expectedRevision":2}`); code != http.StatusOK {
		t.Fatalf("apply = %d %s", code, body)
	}
	eventually(t, "both proposal notices", func() bool {
		code, body := do(t, cfg, admin, http.MethodGet, acmeHost, noticePath, "")
		return code == http.StatusOK && fieldNumber(t, body, "total") == 2
	})
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	for _, name := range []string{"change.proposal_reviewed", "change.proposal_applied"} {
		ev := events.Event{Name: name}
		if err := owner.QueryRowContext(t.Context(),
			`SELECT id, tenant_id, payload FROM platformkit_outbox WHERE name = $1`, name).
			Scan(&ev.ID, &ev.TenantID, &ev.Payload); err != nil {
			t.Fatal(err)
		}
		// The existing memory transport waits for all subscription transactions,
		// so the following count observes completed redeliveries, not a delay.
		for range 3 {
			if err := opts.Transport.Publish(t.Context(), ev); err != nil {
				t.Fatalf("redeliver %s: %v", name, err)
			}
		}
	}
	code, body = do(t, cfg, admin, http.MethodGet, acmeHost, noticePath, "")
	if code != http.StatusOK || fieldNumber(t, body, "total") != 2 {
		t.Errorf("replayed verdict and apply produced extra notices: %d %s", code, body)
	}
}
