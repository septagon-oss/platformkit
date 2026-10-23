package usertest

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func deactivateUser(t *testing.T, f Fixture) {
	u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	for range 2 {
		got, err := f.Service.Deactivate(f.Ctx, f.Tx, u.ID)
		if err != nil {
			t.Fatalf("Deactivate: %v", err)
		}
		if got.Status != contracts.StatusInactive || got.CanSignIn() {
			t.Errorf("a deactivated user is %q and CanSignIn is %v", got.Status, got.CanSignIn())
		}
	}
	published(t, f, contracts.EventInvited, contracts.EventPasswordSet, contracts.EventDeactivated)
}
