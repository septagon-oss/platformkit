package internal_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestNoEventCarriesTheSessionCredential is the regression for the core review of 2026-09-29: auth.logged_in
// published the raw session id, the browser's cookie credential, and an auditor holding only audit:read read
// it back through the audit API and signed in as the administrator (403 → 200). Every payload the outbox holds
// after a login and a logout is scanned for the id in every spelling a reader could turn back into a cookie;
// the login and the logout name the session by the same non-secret SessionRef instead.
func TestNoEventCarriesTheSessionCredential(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users := realUsers()
	svc := internal.NewService(users, &authtest.Notices{}, delivery(&authtest.Mailbox{}))
	seed(t, conn, acme)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)

	var session uuid.UUID
	var payloads []map[string]any
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := users.Invite(ctx, tx, "ada@acme.example.com", "Ada")
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, tx, u.ID, authtest.Password); err != nil {
			return err
		}
		s, _, err := svc.Login(ctx, tx, "ada@acme.example.com", authtest.Password, nobody)
		if err != nil {
			return err
		}
		session = s.ID
		if err := svc.Logout(ctx, tx, s.ID); err != nil {
			return err
		}
		var rows []struct {
			Name    string
			Payload []byte
		}
		if err := tx.DB().Table("platformkit_outbox").Select("name, payload").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			spellings := []string{session.String(), strings.ReplaceAll(session.String(), "-", ""),
				strings.ToUpper(session.String()), hex.EncodeToString(session[:])}
			for _, spelling := range spellings {
				if strings.Contains(string(row.Payload), spelling) {
					t.Errorf("%s carries the session credential (%q): %s", row.Name, spelling, row.Payload)
				}
			}
			if row.Name == contracts.EventLoggedIn || row.Name == contracts.EventLoggedOut {
				var m map[string]any
				if err := json.Unmarshal(row.Payload, &m); err != nil {
					return err
				}
				payloads = append(payloads, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("sign in and out: %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("want one logged_in and one logged_out payload, got %d", len(payloads))
	}
	want := contracts.SessionRef(session)
	for _, m := range payloads {
		if m["sessionRef"] != want {
			t.Errorf("sessionRef = %v, want %s: a login and its logout name one session", m["sessionRef"], want)
		}
		if _, ok := m["sessionId"]; ok {
			t.Errorf("the payload still has a sessionId field: %v", m)
		}
	}
	if want == session.String() || len(want) != 64 {
		t.Fatalf("SessionRef is not the 32-byte digest's hex: %q", want)
	}
}
