package internal_test

// Which tenants may sign in with a passkey alone is a tenant's decision and not a
// deployment's, and modules/auth/module.go claims the usernameless door reads that
// decision per request, out of the tenant's own row in passkey_settings.
//
// Nothing else in this repository checks that claim. A policy read once at wiring
// time would pass every other case here: it would answer the same way for the
// tenant that turned the door on and the tenant that did not, and the only visible
// symptom would be one tenant's administrator writing a setting that changes
// nothing until somebody restarts the process. So this case walks one installation
// in the order a person would meet it:
//
//   - nothing wrote the row, so the door is shut — shut with the reason at an
//     address that exists, not with a 404 that says the route was never mounted,
//     and with no ceremony minted, because a refusal that began a prompt would
//     leave a nonce sitting in a table for two minutes for a tenant that is not
//     being asked for one;
//   - the tenant turns it on, and the *next* request is answered with a ceremony:
//     no restart, no re-wiring, because the answer is read in the transaction the
//     request's own host resolved;
//   - the row that says false is the row that says nothing: a tenant that turned
//     the door on and then off again is refused at the same cost, which is the
//     half a read that defaulted a missing row to true would get wrong.
//
// The row is written here with SQL rather than through a command because no
// command exists yet: the control-plane surface that writes it is the product's
// share (see the delivery's Limits), and what this module promises is the read.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
)

func TestTheUsernamelessDoorIsTheTenantsOwnRow(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	const door = "/api/v1/auth/login/passkey/begin"

	// Before: the door exists and is shut.
	res := call(t, router, http.MethodPost, door, "")
	if res.Code != http.StatusForbidden {
		t.Fatalf("the usernameless door at a tenant that turned nothing on = %d %s, want 403",
			res.Code, res.Body.String())
	}
	n, _ := ceremonies(t, conn)
	if n != 0 {
		t.Errorf("the refused door minted %d ceremonies, want 0: a tenant that does not offer this door has no reason to hold a nonce", n)
	}

	// The tenant turns it on, and the very next request is the ceremony.
	setPasskeySignIn(t, conn, true)
	res = call(t, router, http.MethodPost, door, "")
	if res.Code != http.StatusOK {
		t.Fatalf("the same door after the tenant turned it on = %d %s, want 200",
			res.Code, res.Body.String())
	}
	var challenge struct {
		Ceremony string          `json:"ceremony"`
		Options  json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &challenge); err != nil {
		t.Fatalf("the challenge the door answers with is not the JSON a browser is handed: %v (%s)", err, res.Body.String())
	}
	if challenge.Ceremony == "" || len(challenge.Options) == 0 {
		t.Errorf("the door answered %s, want a ceremony id and the options navigator.credentials.get takes", res.Body.String())
	}
	rows, kind := ceremonies(t, conn)
	if rows != 1 {
		t.Fatalf("the begun ceremony left %d rows, want 1", rows)
	}
	if kind != "sign-in" {
		t.Errorf("the begun ceremony is kind %q, want %q: which door a prompt belongs to is written when it is begun", kind, "sign-in")
	}

	// And turning it back off is the same refusal as never having turned it on.
	setPasskeySignIn(t, conn, false)
	if res := call(t, router, http.MethodPost, door, ""); res.Code != http.StatusForbidden {
		t.Errorf("the door after the tenant turned it off again = %d %s, want 403", res.Code, res.Body.String())
	}
}

// setPasskeySignIn is the write the product's control plane will make: the
// tenant's own row, turned from inside that tenant's transaction so the row-level
// security of 000035 is what decides whether this can reach another tenant.
func setPasskeySignIn(t *testing.T, conn *db.Conn, enable bool) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(
			"INSERT INTO passkey_settings (tenant_id, sign_in) VALUES (?, ?)"+
				" ON CONFLICT (tenant_id) DO UPDATE SET sign_in = EXCLUDED.sign_in",
			acme.ID, enable).Error
	})
	if err != nil {
		t.Fatalf("turn passkey sign-in %v for acme: %v", enable, err)
	}
}

// ceremonies is what acme holds in flight and which door it was begun at. The
// read is inside acme's own transaction because that is how the table is read at
// all: 000035 forces row-level security on it, so a count taken outside a tenant
// is a question about a table this installation never reads that way.
func ceremonies(t *testing.T, conn *db.Conn) (int64, string) {
	t.Helper()
	var n int64
	var kind string
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Table("passkey_challenges").Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		return tx.DB().Table("passkey_challenges").Select("kind").Row().Scan(&kind)
	})
	if err != nil {
		t.Fatalf("read acme's ceremonies: %v", err)
	}
	return n, kind
}
