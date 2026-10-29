package auth_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth"
)

func TestAuthRequiresACompleteGrantDependencyPair(t *testing.T) {
	authorizer, permissions, _ := auth.NewGrants(auth.GrantsDeps{})
	for _, deps := range []auth.Deps{{Authorization: authorizer}, {Permissions: permissions}} {
		if got := refusal(deps); !strings.Contains(got, "both Authorization and Permissions") {
			t.Fatalf("partial grants refusal = %q", got)
		}
	}
	for _, deps := range []auth.Deps{{}, {Authorization: authorizer, Permissions: permissions}} {
		svc, _ := auth.New(deps)
		if held, err := svc.Permissions(t.Context(), db.Tx[db.Tenant]{}, nil); err != nil || len(held) != 0 {
			t.Fatalf("empty grants = %v, %v", held, err)
		}
	}
}
