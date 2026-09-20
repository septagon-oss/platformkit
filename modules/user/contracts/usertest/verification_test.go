package usertest

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestFakeVerificationChecksTheCurrentPasswordAndEmail(t *testing.T) {
	// No role system: this file is about verification, and that is the world it
	// wants. A case that means to exercise the floor asks for one.
	f := NewFake()
	var tx db.Tx[db.Tenant]
	u, err := f.RegisterUnverified(t.Context(), tx, contracts.PasswordRegistration{Email: "ada@example.com", Password: good})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"password", "email"} {
		f.mu.Lock()
		row := *u
		if change == "password" {
			row.PasswordHash = ""
		} else {
			row.Email = "changed@example.com"
		}
		f.users[u.ID] = row
		f.mu.Unlock()
		if _, err := f.VerifyEmail(t.Context(), tx, u.ID, u.Email); !errors.Is(err, crud.ErrConflict) {
			t.Fatalf("verification after %s change = %v", change, err)
		}
	}
	if len(f.Published()) != 1 {
		t.Fatal("a refused verification published an event")
	}
}
