package internal_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	auditcontracts "github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestPasskeyUseAuditNamesThePersonWhoSignedIn(t *testing.T) {
	t.Run("signed_out", func(t *testing.T) { passkeyUseActor(t, false) })
	t.Run("another_account", func(t *testing.T) { passkeyUseActor(t, true) })
}

func passkeyUseActor(t *testing.T, alreadySignedIn bool) {
	t.Helper()
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations, audit.Migrations)
	router, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false)
	trail := audit.Module(audit.Deps{})
	trail.Routes(api.Surfaces("audit"))
	ada := person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	key := newSoftAuthenticator(t, host)
	key.counter = 1
	if code, body := enrolPasskey(t, router, session, key); code != http.StatusCreated {
		t.Fatalf("enrol = %d %s", code, body)
	}
	setDoor(t, router, session, true)
	begun := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", "")
	if begun.Code != http.StatusOK {
		t.Fatalf("begin = %d %s", begun.Code, begun.Body.String())
	}
	ceremony, challenge := challengeOf(t, begun.Body.Bytes())
	key.counter = 2
	body, err := json.Marshal(map[string]any{"ceremony": ceremony, "response": key.asserted(t, challenge)})
	if err != nil {
		t.Fatal(err)
	}
	var edit []func(*http.Request)
	if alreadySignedIn {
		person(t, conn, "bob@acme.localhost", contracts.RoleMember)
		edit = append(edit, withSession(signIn(t, router, "bob@acme.localhost")))
	}
	res := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/verify", string(body), edit...)
	if res.Code != http.StatusOK || sessionCookie(res) == "" {
		t.Fatalf("the passkey sign-in = %d %s, want a session", res.Code, res.Body.String())
	}

	// Deliver the committed outbox envelope to the composed audit handler twice.
	// This uses the real subscriber and its ordinary tenant transaction.
	ctx := tenancy.WithTenant(t.Context(), acme)
	var event events.Event
	err = db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var actor sql.NullString
		err := tx.DB().Raw(`SELECT id, name, tenant_id, payload, created_at, actor
 FROM platformkit_outbox WHERE name = ?`, contracts.EventFactorUsed).Row().Scan(
			&event.ID, &event.Name, &event.TenantID, &event.Payload, &event.At, &actor)
		if err != nil {
			return err
		}
		if actor.Valid {
			event.Actor, err = uuid.Parse(actor.String)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return trail.Subscriptions[0].Handler(ctx, tx, event)
		}); err != nil {
			t.Fatal(err)
		}
	}
	read := func(query string) []*auditcontracts.Event {
		t.Helper()
		res := call(t, router, http.MethodGet, "/api/v1/audit/events?name="+contracts.EventFactorUsed+query, "", withSession(session))
		if res.Code != http.StatusOK {
			t.Fatalf("read the audit trail = %d %s", res.Code, res.Body.String())
		}
		var page struct {
			Items []*auditcontracts.Event `json:"items"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page.Items
	}
	rows := read("")
	if len(rows) != 1 {
		t.Fatalf("the replayed passkey use left %d audit rows, want 1", len(rows))
	}
	if rows[0].Actor == nil || *rows[0].Actor != ada {
		t.Errorf("successful passkey use audit actor = %v, want the authenticated person %s", rows[0].Actor, ada)
	}
	if rows := read("&actor=" + ada.String()); len(rows) != 1 {
		t.Errorf("the authenticated person's passkey-use audit query returned %d rows, want 1", len(rows))
	}
}
