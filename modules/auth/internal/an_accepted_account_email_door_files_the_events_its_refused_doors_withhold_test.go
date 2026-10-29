package internal_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// TestAnAcceptedAccountEmailDoorFilesTheEventsItsRefusedDoorsWithhold asks each event
// name this package counts as "what an account-email door would have filed" whether a
// door of that kind, standing where mail is delivered, files it.
//
// Every no-event claim in this package is a count of these four names: a refused door
// passes for as long as it files none of them. That the count could see one at all is a
// separate fact, and the one nothing else asks here — the outbox is pinned as one tenant's
// read and as the installation's, never as the set of names a flow actually produces. A
// door that moved its event to another name, or gained one nobody counts, would leave the
// refused-door cases counting zero of a name nobody files, which is the answer a refusal
// gives and is not the same thing.
//
// Each leg mounts the composition that owns its door, makes the calls that door answers,
// and accepts nothing below one outbox row filed under the name that door owes. The calls
// are the reachability half: a leg that got no acceptance never asked its door anything,
// and says so rather than reporting an empty outbox.
func TestAnAcceptedAccountEmailDoorFilesTheEventsItsRefusedDoorsWithhold(t *testing.T) {
	for _, door := range []struct {
		// name is the event this door owes the outbox when it accepts, which is the
		// name the refused-door cases count.
		name string
		// registration is the registrar that mounts the queue door; wires picks the
		// composition that mounts the confirmation door and its "no link" twin.
		registration bool
		wires        func(*auth.Deps)
		ask          func(t *testing.T, router chi.Router)
	}{
		{
			name:         contracts.EventRegistrationRequested,
			registration: true,
			ask: func(t *testing.T, router chi.Router) {
				accepts(t, router, "/api/v1/public/auth/register",
					`{"email":"queued@example.com","displayName":"Queued"}`, "203.0.113.31")
			},
		},
		{
			name:  contracts.EventVerificationRequested,
			wires: emailSignup,
			ask: func(t *testing.T, router chi.Router) {
				accepts(t, router, "/api/v1/public/auth/register",
					approvalBody(t, "verify@example.com", nil), "203.0.113.32")
				accepts(t, router, "/api/v1/public/auth/resend-verification",
					`{"email":"verify@example.com"}`, "203.0.113.33")
			},
		},
		{
			name:  contracts.EventNoLinkRequested,
			wires: emailSignup,
			ask: func(t *testing.T, router chi.Router) {
				// The same address twice: the second sign-up is the one with nobody new
				// behind it, and it owes a message of its own (internal/no_link.go).
				accepts(t, router, "/api/v1/public/auth/register",
					approvalBody(t, "already@example.com", nil), "203.0.113.34")
				accepts(t, router, "/api/v1/public/auth/register",
					approvalBody(t, "already@example.com", nil), "203.0.113.35")
			},
		},
		{
			name: contracts.EventResetRequested,
			ask: func(t *testing.T, router chi.Router) {
				accepts(t, router, "/api/v1/public/auth/password/forgot",
					`{"email":"forgot@example.com"}`, "203.0.113.36")
			},
		},
	} {
		t.Run(door.name, func(t *testing.T) {
			_, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
			var wires []func(*auth.Deps)
			if door.wires != nil {
				wires = append(wires, door.wires)
			}
			router, _, _ := mountConfigured(t, conn, auth.OIDC{}, door.registration, wires...)
			door.ask(t, router)
			if rows := outboxRowsFiled(t, conn, door.name); rows == 0 {
				t.Errorf("no accepting account-email door filed a %q row, so counting that name answers for nothing: it is the name the refused-door cases read",
					door.name)
			}
		})
	}
}

// accepts posts one public account-email call from one address and refuses to let the
// count rest on a request that was never answered: the door has to give the acceptance its
// composition can honour, or the leg has not asked it anything.
func accepts(t *testing.T, router chi.Router, path, body, addr string) {
	t.Helper()
	res := call(t, router, http.MethodPost, path, body, sameSite, from(addr))
	if res.Code != http.StatusAccepted && res.Code != http.StatusOK {
		t.Fatalf("%s answered %d %s, want an acceptance this composition can honour", path, res.Code, res.Body.String())
	}
}

// outboxRowsFiled counts the outbox rows filed under one event name, as the tenant under
// test. Nothing discharges the outbox in this file: no subscription runs, so a row a door
// filed is still there when the count asks.
func outboxRowsFiled(t *testing.T, conn *db.Conn, name string) int64 {
	t.Helper()
	var rows int64
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name = ?", name).Row().Scan(&rows)
	})
	if err != nil {
		t.Fatalf("count the %q rows in the outbox: %v", name, err)
	}
	return rows
}
