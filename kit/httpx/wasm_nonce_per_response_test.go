package httpx_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestEachDeclaredResponseNamesItsOwnNonce: declaring WebAssembly widens
// script-src by one token and leaves the nonce a per-response value. Two answers
// of the same declaring page carry two different nonces, each policy names the
// nonce its own body carries, and neither names the other's.
func TestEachDeclaredResponseNamesItsOwnNonce(t *testing.T) {
	api, router, _ := setup(t)
	wasmPage(api.Surfaces(probe).App, "read-editor", "/editor", true)

	first := get(t, router, page(api, "/editor"))
	second := get(t, router, page(api, "/editor"))
	for _, res := range []int{first.Code, second.Code} {
		if res != http.StatusOK {
			t.Fatalf("the declaring page answered %d", res)
		}
	}
	a, b := nonceOf(t, first), nonceOf(t, second)
	if a == b {
		t.Fatalf("two responses of the declaring page share the nonce %q", a)
	}
	cspA, cspB := cspOf(t, first, appPolicyWASM), cspOf(t, second, appPolicyWASM)
	if strings.Contains(cspA, b) || strings.Contains(cspB, a) {
		t.Errorf("a policy names another response's nonce: %q / %q", cspA, cspB)
	}
}
