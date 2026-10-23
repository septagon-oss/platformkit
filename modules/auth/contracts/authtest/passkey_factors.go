package authtest

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func listPasskeyWithTOTP(t *testing.T, f PasskeyFixture) {
	ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	f.AddFactor(ada, FactorTOTP, "", "totp-secret")
	key := enrol(t, f, ada, theCred, "Office laptop")
	list, err := f.Factors.ListFactors(f.Ctx, f.Tx, ada)
	if err != nil {
		t.Fatalf("ListFactors: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("this person holds %d factors, want a passkey and a totp", len(list))
	}
	kinds := []string{list[0].Kind, list[1].Kind}
	if !slices.Contains(kinds, FactorPasskey) || !slices.Contains(kinds, FactorTOTP) {
		t.Errorf("the list holds kinds %v, want both passkey and totp", kinds)
	}
	for _, factor := range list {
		if factor.Kind == FactorTOTP && factor.Name != "" {
			t.Errorf("a totp is named %q, where an authenticator app has no name to give", factor.Name)
		}
		if factor.Kind == FactorPasskey && factor.Name != "Office laptop" {
			t.Errorf("the passkey is named %q, want the label its owner typed", factor.Name)
		}
		if factor.ID != key.ID && factor.Kind != FactorTOTP {
			t.Errorf("the list holds a second passkey %+v", factor)
		}
		if strings.Contains(strings.ToLower(factor.Name), "credential") {
			t.Errorf("the name carries ceremony material: %q", factor.Name)
		}
	}
	published(t, f.Fixture, contracts.EventFactorEnrolled)
}

func preserveLastFactor(t *testing.T, f PasskeyFixture) {
	ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	key := enrol(t, f, ada, theCred, "Only way in")
	if err := f.Factors.WithdrawFactor(f.Ctx, f.Tx, ada, key.ID); !errors.Is(err, contracts.ErrLastFactor) {
		t.Errorf("withdrawing the only passkey = %v, want ErrLastFactor", err)
	}
	list, _ := f.Factors.ListFactors(f.Ctx, f.Tx, ada)
	if len(list) != 1 {
		t.Errorf("the refusal left %d factors, want the one it refused to take", len(list))
	}
	published(t, f.Fixture, contracts.EventFactorEnrolled)
}

func withdrawEitherFactor(t *testing.T, f PasskeyFixture) {
	ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	totp := f.AddFactor(ada, FactorTOTP, "", "totp-secret")
	key := enrol(t, f, ada, theCred, "Office laptop")
	if err := f.Factors.WithdrawFactor(f.Ctx, f.Tx, ada, key.ID); err != nil {
		t.Fatalf("withdraw the passkey while a totp is still held: %v", err)
	}
	list, _ := f.Factors.ListFactors(f.Ctx, f.Tx, ada)
	if len(list) != 1 || list[0].Kind != FactorTOTP {
		t.Fatalf("withdrawing the passkey left %+v, want the totp", list)
	}
	// And the rule holds at the end of the road too: two factors became
	// one, and one is the last.
	if err := f.Factors.WithdrawFactor(f.Ctx, f.Tx, ada, totp); !errors.Is(err, contracts.ErrLastFactor) {
		t.Errorf("withdrawing what the first withdrawal left = %v, want ErrLastFactor", err)
	}
	published(t, f.Fixture, contracts.EventFactorEnrolled, contracts.EventFactorWithdrawn)
}

func refuseUnknownFactor(t *testing.T, f PasskeyFixture) {
	ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	f.AddFactor(ada, FactorTOTP, "", "totp-secret")
	key := enrol(t, f, ada, theCred, "Office laptop")
	other := f.User("bob@acme.example.com", Password, contracts.RoleMember)
	for _, id := range []uuid.UUID{uuid.New(), key.ID} {
		if err := f.Factors.WithdrawFactor(f.Ctx, f.Tx, other, id); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("withdrawing %s from somebody who does not hold it = %v, want ErrNotFound", id, err)
		}
	}
	published(t, f.Fixture, contracts.EventFactorEnrolled)
}

func requireFactorForRecovery(t *testing.T, f PasskeyFixture) {
	ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	if _, err := f.Factors.RotateRecoveryCodes(f.Ctx, f.Tx, ada); !errors.Is(err, contracts.ErrNoFactor) {
		t.Fatalf("codes for a person with no factor = %v, want ErrNoFactor", err)
	}
	enrol(t, f, ada, theCred, "Office laptop")
	codes, err := f.Factors.RotateRecoveryCodes(f.Ctx, f.Tx, ada)
	if err != nil {
		t.Fatalf("codes after a passkey: %v", err)
	}
	if len(codes) != contracts.RecoveryCodes {
		t.Errorf("the set holds %d codes, want %d", len(codes), contracts.RecoveryCodes)
	}
	published(t, f.Fixture, contracts.EventFactorEnrolled, contracts.EventRecoveryCodesIssued)
}
