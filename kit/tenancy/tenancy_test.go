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
// restriction is the whole barrier.
func TestSystemTokenIsMintedByTheKernel(t *testing.T) {
	const reason = "nightly billing rollup"
	var token tenancy.SystemToken = syscap.NewSystemToken(reason)
	if token.Reason() != reason {
		t.Errorf("Reason = %q, want %q", token.Reason(), reason)
	}

	// SystemToken is an interface with an unexported method, so no package can
	// implement it and the zero value is nil — the one forgery Go still allows,
	// and the one db.RunSystem refuses.
	var forged tenancy.SystemToken
	if forged != nil {
		t.Error("the zero token is not nil, so something else can be one")
	}
}

// TestAReasonlessTokenIsRefusedWhereItIsMinted, not where it is used: a
// capability nobody can explain is a mistake at the minting site.
func TestAReasonlessTokenIsRefusedWhereItIsMinted(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("syscap.NewSystemToken accepted an empty reason")
		}
	}()
	syscap.NewSystemToken("")
}
