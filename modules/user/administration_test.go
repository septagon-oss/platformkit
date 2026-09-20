package user_test

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// answer is a role system with one name in it, enough to be worth comparing.
func answer(context.Context, db.Tx[db.Tenant]) ([]string, error) {
	return []string{"owner"}, nil
}

// TestDepsCompareWithoutPanic pins why AdministrationFunc is taken by pointer.
//
// user.Deps was struct{} at v1.1.0, so it published as comparable: a consumer may
// write deps == other or use one as a map key. A struct holding a func is not
// comparable, and an interface quietly holding a func value is worse — it satisfies
// a static checker and panics the first time two Deps are compared, because
// comparing interfaces compares their dynamic values. Every form below has to be
// ordinary Go, not Go that compiles and detonates in a caller.
func TestDepsCompareWithoutPanic(t *testing.T) {
	zero, also := user.Deps{}, user.Deps{}
	if zero != also {
		t.Error("two empty Deps are not equal; the published comparability is gone")
	}

	shared := &usercontracts.AdministrationFunc{Ask: answer}
	same := user.Deps{Administration: shared}
	if same != (user.Deps{Administration: shared}) {
		t.Error("two Deps holding the same adapter are not equal")
	}
	if same == zero {
		t.Error("a wired Deps equals an empty one")
	}
	// Pointers are distinct values, so two adapters over the same function are
	// two wirings. That is the honest answer and the one a map key needs.
	if same == (user.Deps{Administration: &usercontracts.AdministrationFunc{Ask: answer}}) {
		t.Error("two separate adapters compare equal; they are separate wirings")
	}
	seen := map[user.Deps]string{same: "held"}
	if seen[user.Deps{Administration: shared}] != "held" {
		t.Error("a Deps is not usable as a map key")
	}
}

// TestAnAdapterWithNothingToAskIsRefused is the failure a nil check alone lets
// through: &AdministrationFunc{} satisfies Deps.Administration, is not a nil
// interface, and would answer "no role in this tenant grants role:manage" to the
// one question the floor asks — read as "refuse nothing", the missing floor
// arriving through a field that looks wired. Refused twice, at composition (main
// has nowhere to report to) and by the method returning an error rather than an
// empty list, so an implementation composed elsewhere cannot grant a pass either.
func TestAnAdapterWithNothingToAskIsRefused(t *testing.T) {
	unwired := []user.Deps{
		{Administration: &usercontracts.AdministrationFunc{}},
		{Administration: (*usercontracts.AdministrationFunc)(nil)},
	}
	for i, deps := range unwired {
		func() {
			defer func() {
				reason, ok := recover().(string)
				if !ok || !strings.Contains(reason, "Ask") {
					t.Fatalf("form %d panicked with %v, want the message naming the missing Ask", i, reason)
				}
			}()
			user.Module(deps)
			t.Errorf("form %d composed an adapter with nothing to ask", i)
		}()
	}

	names, err := (&usercontracts.AdministrationFunc{}).Administering(t.Context(), db.Tx[db.Tenant]{})
	if err == nil {
		t.Fatal("an adapter with no Ask answered instead of failing")
	}
	if names != nil {
		t.Errorf("an adapter with no Ask answered %v; an error must not carry names", names)
	}
}
