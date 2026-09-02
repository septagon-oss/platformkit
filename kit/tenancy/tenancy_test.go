package tenancy_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestContextRoundTrip(t *testing.T) {
	ctx := t.Context()
	if _, ok := tenancy.FromContext(ctx); ok {
		t.Fatal("an empty context carries a tenant")
	}
	want := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme Corp"}
	got, ok := tenancy.FromContext(tenancy.WithTenant(ctx, want))
	if !ok {
		t.Fatal("WithTenant did not put the tenant on the context")
	}
	if got != want {
		t.Errorf("FromContext = %+v, want %+v", got, want)
	}
}

// TestSystemTokenIsMintedByTheKernel: kit/tenancy is inside kit/, so it may
// import kit/internal/syscap. A business module may not, and that import
// restriction is the whole barrier — see also the zero value below.
func TestSystemTokenIsMintedByTheKernel(t *testing.T) {
	const reason = "nightly billing rollup"
	var token tenancy.SystemToken = syscap.NewSystemToken(reason)
	if token.Reason() != reason {
		t.Errorf("Reason = %q, want %q", token.Reason(), reason)
	}

	// Go lets any package write tenancy.SystemToken{}, so the type alone does
	// not close the door; the minted token is the one with a reason, and
	// db.RunSystem refuses the other.
	var forged tenancy.SystemToken
	if forged.Reason() != "" {
		t.Errorf("the zero token has a reason: %q", forged.Reason())
	}
}
