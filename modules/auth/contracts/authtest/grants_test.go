package authtest_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

func TestIndependentGrantFakeConforms(t *testing.T) {
	authtest.RunGrants(t, func(t *testing.T, run func(authtest.GrantFixture)) {
		roles := authtest.NewFake(usertest.NewFake())
		run(authtest.GrantFixture{Ctx: t.Context(), Policy: authtest.GrantPolicy{Roles: roles, Tenant: tenancy.Tenant{ID: uuid.New()}}, Role: roles.Grant, Published: roles.Published})
	})
}
