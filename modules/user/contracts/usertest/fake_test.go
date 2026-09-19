package usertest_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

// TestFakeConforms runs the suite against the fake. This is what makes the fake
// worth having: the auth module tests against it, so it is testing against the
// same rules internal/service_test.go proves the real service keeps.
func TestFakeConforms(t *testing.T) {
	usertest.RunService(t, func(t *testing.T, run func(usertest.Fixture)) {
		// Administering, so the fake's tenant has the role system the
		// floor cases are about; the real service's harness wires the same
		// answer, which is what makes the two comparable. NewFake would have
		// answered "no role system", and every floor case would pass vacuously.
		fake := usertest.NewFakeWithAdministration([]string{usertest.Administering})
		run(usertest.Fixture{Ctx: t.Context(), Service: fake, Published: fake.Published,
			Delete: func(id uuid.UUID) error { return fake.Delete(t.Context(), id) }})
	})
}

// TestTheTwoConstructorsAnswerDifferentTenants is the published-signature case.
//
// NewFake keeps the signature it published in v1.1.0 and keeps what it meant: a
// store with no role system, refusing nothing. Asking for the floor is the other
// constructor's job — so the difference has to be observable, or one of the two is
// decoration and a consumer's floor test passes without testing anything.
func TestTheTwoConstructorsAnswerDifferentTenants(t *testing.T) {
	// The floor only guards somebody who could sign in, so both fakes get an
	// active person holding the administering role and are then asked the same
	// question: may the last of them be stripped?
	ask := func(t *testing.T, fake *usertest.Fake) error {
		t.Helper()
		ctx, tx, system := t.Context(), db.Tx[db.Tenant]{}, db.Tx[db.System]{}
		who, err := fake.Provision(ctx, system, uuid.New(), "ada@acme.test", "Ada",
			"correct horse battery staple", []string{usertest.Administering})
		if err != nil {
			t.Fatalf("Provision: %v", err)
		}
		_, err = fake.SetRoles(ctx, tx, who.ID, nil)
		return err
	}

	if err := ask(t, usertest.NewFake()); err != nil {
		t.Errorf("the plain fake refused a bare removal (%v); it is a tenant with no role system, and that is what its callers at v1.1.0 got", err)
	}
	if err := ask(t, usertest.NewFakeWithAdministration([]string{usertest.Administering})); err == nil {
		t.Error("the fake with a role system let the last administrator be emptied")
	}
}
