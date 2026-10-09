package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// relayWithRequest delivers every outbox row to auth's subscriptions carrying the
// request id the row kept, the way the relay hands it to a worker. worker() drops
// it, and a mail with no request id behind it is a mail the delivery door can never
// be asked about.
func relayWithRequest(t *testing.T, conn *db.Conn) {
	t.Helper()
	type pending struct {
		Name      string
		Payload   []byte
		RequestID *string
	}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var rows []pending
		if err := tx.DB().Table("platformkit_outbox").Order("created_at, id").Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			requestID := ""
			if r.RequestID != nil {
				requestID = *r.RequestID
			}
			for _, s := range subs {
				if s.Name != r.Name {
					continue
				}
				err := s.Handler(ctx, tx, events.Event{
					ID: uuid.New(), Name: r.Name, TenantID: acme.ID, Payload: r.Payload, RequestID: requestID,
				})
				if err != nil {
					return err
				}
			}
		}
		return tx.DB().Exec("DELETE FROM platformkit_outbox").Error
	})
	if err != nil {
		t.Fatalf("relay the outbox: %v", err)
	}
}

// TestTheMailDeliveryDoorDoesNotTellAKnownAddressFromAnUnknownOne is the promise
// the forgotten-password route already keeps — it says the same thing to
// everybody — kept by the door that reports on the mail that route caused.
//
// The caller of the forgotten-password route is answered with its own request id
// (X-Request-ID), so whoever typed the address holds the handle the door is asked
// with. If the door then answers `sent` for an address that has an account and
// `pending` for one that has none, the neutral acknowledgment is undone one call
// later and the door is a list of who has an account here.
func TestTheMailDeliveryDoorDoesNotTellAKnownAddressFromAnUnknownOne(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.Mails = mailLedger()
	})
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)

	forgot := func(email string) string {
		res := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"`+email+`"}`)
		if res.Code != http.StatusOK {
			t.Fatalf("forgot %s=%d %s, want 200", email, res.Code, res.Body.String())
		}
		id := res.Header().Get("X-Request-ID")
		if id == "" {
			t.Fatalf("forgot %s was answered with no request id", email)
		}
		return id
	}
	known, unknown := forgot("ada@acme.localhost"), forgot("nobody@acme.localhost")
	relayWithRequest(t, conn)
	// The worker ran and mailed the one address that has an account: the door has
	// something to say about one of the two calls.
	if got := len(mailbox.Sent()); got != 1 {
		t.Fatalf("mails after the relay=%d, want the one link for the address that is here", got)
	}

	if a, b := ask(router, known), ask(router, unknown); a != b {
		t.Errorf("the door answers %q about the address with an account and %q about the one without:"+
			" asked with the id the forgotten-password route answered, it says who has an account", a, b)
	}
}

// TestTheMailDeliveryDoorDoesNotTellASignUpForATakenAddressFromANewOne is the same
// promise at the sign-up form, whose description says "existing accounts remain
// unchanged" behind a neutral acknowledgment.
func TestTheMailDeliveryDoorDoesNotTellASignUpForATakenAddressFromANewOne(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations, audit.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(d *auth.Deps) {
		d.Mails = mailLedger()
	})
	person(t, conn, "taken@acme.localhost", contracts.RoleMember)

	register := func(email string) string {
		res := call(t, router, http.MethodPost, "/api/v1/public/auth/register", approvalBody(t, email, nil))
		if res.Code != http.StatusAccepted {
			t.Fatalf("register %s=%d %s, want 202", email, res.Code, res.Body.String())
		}
		return res.Header().Get("X-Request-ID")
	}
	taken, fresh := register("taken@acme.localhost"), register("fresh@acme.localhost")
	relayWithRequest(t, conn)
	if got := len(mailbox.Sent()); got == 0 {
		t.Fatal("no mail left after two sign-ups, so the door has nothing to say about either")
	}
	if a, b := ask(router, taken), ask(router, fresh); a != b {
		t.Errorf("the door answers %q about a sign-up for a taken address and %q about a new one:"+
			" the neutral acknowledgment is undone one call later", a, b)
	}
}

// ask is the delivery door's one word about one request id.
func ask(router http.Handler, requestID string) string {
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/v1/public/auth/mail-delivery",
		strings.NewReader(`{"requestId":"`+requestID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	router.ServeHTTP(res, req)
	var out struct {
		State string `json:"state"`
	}
	_ = json.NewDecoder(res.Result().Body).Decode(&out)
	return strconv.Itoa(res.Code) + " " + out.State
}
