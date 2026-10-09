package contracts_test

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestVerificationMailAndPasswordResetKeepTheirDeclaredOutagePolicies(t *testing.T) {
	// No connection is the documented down-store outcome. Exercise the real
	// adapter so the two composers receive the same error classification.
	l := contracts.NewLimiter(limit.Postgres(httpx.ConnFrom))
	allowed, err := l.VerificationMail(t.Context(), "ada@example.com")
	if allowed || !errors.Is(err, limit.ErrNoConnection) {
		t.Errorf("VerificationMail without a store = %v, %v; want refused with ErrNoConnection", allowed, err)
	}
	if !l.Requested(t.Context(), "203.0.113.1") {
		t.Error("password reset request without a store was refused; want the declared fail-open policy")
	}
}
