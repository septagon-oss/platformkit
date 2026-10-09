package internal_test

// The two anonymous begin legs write a ceremony row per request and declare 429
// among their answers; the second-factor leg also pays the password hash for an
// address that does not exist. A public write is rate-limited (decision 0011):
// one address asking without end is answered 429 before the table grows by one
// row per request.

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
)

func TestAPasskeyPromptFromOneAddressIsRateLimited(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	setPasskeySignIn(t, conn, true)

	const asks = 30 // above every per-address cap contracts.Limiter declares
	for _, door := range []struct{ path, body string }{
		{"/api/v1/auth/challenge/passkey/begin", `{"email":"nobody@acme.localhost"}`},
		{"/api/v1/auth/login/passkey/begin", ""},
	} {
		limited := 0
		for range asks {
			res := call(t, router, http.MethodPost, door.path, door.body)
			switch res.Code {
			case http.StatusOK:
			case http.StatusTooManyRequests:
				limited++
			default:
				t.Fatalf("%s = %d %s, want 200 or 429", door.path, res.Code, res.Body.String())
			}
		}
		if limited == 0 {
			t.Errorf("%d prompts from one address at %s were all answered 200, want 429 once the address is over its cap", asks, door.path)
		}
	}
	if n, _ := ceremonies(t, conn); n >= 2*asks {
		t.Errorf("one address minted %d ceremony rows in %d requests, want fewer: a limited request writes nothing", n, 2*asks)
	}
}
