package internal_test

// The second-factor begin leg is public and takes an address. Its route says it
// "cannot be asked who has one", and its command says the three doors "cannot be
// told apart by a stopwatch". This case asks the stopwatch, the way
// TestTheForgottenPasswordRouteCostsTheSameEitherWay does: interleaved pairs, one
// address that has an account and one that does not, and a sign test — equal
// costs make the known request the slower one in about half the pairs.

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestAPasskeyPromptCostsTheSameForAnyAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("the clock needs samples")
	}
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)

	begin := func(email string, i int) (time.Duration, int) {
		// A fresh caller address each time, so a per-address cap is not what is measured.
		at := time.Now()
		res := call(t, router, http.MethodPost, "/api/v1/auth/challenge/passkey/begin",
			`{"email":"`+email+`"}`, from(fmt.Sprintf("198.51.%d.%d", 100+i/200, i%200)))
		return time.Since(at), res.Code
	}
	const pairs = 200
	slower := 0
	for i := range pairs {
		k, kc := begin("ada@acme.localhost", i*2)
		u, uc := begin("nobody@acme.localhost", i*2+1)
		if kc != http.StatusOK || uc != http.StatusOK {
			t.Fatalf("pair %d answered %d and %d, want 200 for both", i, kc, uc)
		}
		if k > u {
			slower++
		}
	}
	if share := float64(slower) / pairs; share < 0.359 || share > 0.641 {
		t.Errorf("the address with an account was the slower in %d of %d interleaved pairs; equal costs split near half, so this leg tells a stranger who has an account", slower, pairs)
	}
}
