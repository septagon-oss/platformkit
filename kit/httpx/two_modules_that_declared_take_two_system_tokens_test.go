package httpx_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// declaring is the composition as kit/app states it: each module that crosses
// tenants names itself and the work, and the kernel hands out one credential per
// entry. These are the two entries this repository's modules would write.
func declaring() map[string]string {
	return map[string]string{
		"billing": "read every tenant's subscription before a plan is removed",
		"file":    "sweep stored bytes no row references, across every tenant",
	}
}

func unwiredAPI(access map[string]string) (*httpx.API, func() error) {
	store := cache.Memory("pkit")
	f := &fixture{}
	api, _ := httpx.New(httpx.Options{
		Unwired: true, Cache: store, Tenants: f, Authorize: f, Authenticate: f.authenticate,
		SystemAccess: access,
	})
	return api, store.Close
}

func TestTwoModulesThatDeclaredTakeTwoSystemTokens(t *testing.T) {
	api, closeStore := unwiredAPI(declaring())
	defer func() { _ = closeStore() }()

	billing := api.Surfaces("billing").App.SystemToken()
	file := api.Surfaces("file").Ops.SystemToken()
	if billing == nil || file == nil {
		t.Fatalf("a declaring module was handed no credential: billing %v, file %v", billing, file)
	}
	if billing == file {
		t.Fatal("two modules hold one credential, so the audit trail cannot tell them apart")
	}
	for _, want := range []struct{ module, reason string }{
		{"billing", "read every tenant's subscription before a plan is removed"},
		{"file", "sweep stored bytes no row references, across every tenant"},
	} {
		taken := api.Surfaces(want.module).App.SystemToken()
		if taken.Module() != want.module {
			t.Errorf("the credential names module %q and not %q", taken.Module(), want.module)
		}
		if taken.Reason() != want.reason {
			t.Errorf("the credential carries %q and not the declared %q", taken.Reason(), want.reason)
		}
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("a composition whose modules declared their cross-tenant work does not boot: %v", err)
	}
}

// TestEveryRegistrationPassAnswersTheSameSystemToken is what keeps the fourth
// registration — the API that will serve — from holding a different credential
// than the three the gates read: kit/app compares the registrations route by
// route, and a Routes callback that minted freshly on each pass would differ in
// what it did rather than in what it mounted.
func TestEveryRegistrationPassAnswersTheSameSystemToken(t *testing.T) {
	api, closeStore := unwiredAPI(declaring())
	defer func() { _ = closeStore() }()

	first := api.Surfaces("file").Ops.SystemToken()
	for pass := 2; pass <= 4; pass++ {
		if again := api.Surfaces("file").Ops.SystemToken(); again != first {
			t.Fatalf("registration %d minted a second credential for one module", pass)
		}
	}
}

// TestABorrowedNamespaceTakesNoSystemToken is why the mint answers the router's
// owner and not its namespace: a shell's page mounted beside the tenant screens
// is still the shell's page, and a credential is not something a borrowed
// address picks up.
func TestABorrowedNamespaceTakesNoSystemToken(t *testing.T) {
	api, closeStore := unwiredAPI(declaring())
	defer func() { _ = closeStore() }()

	api.Surfaces("tenant") // the composition registers a namespace per module it wires
	borrowed := api.Surfaces("billing").App.ForModule("tenant")
	if borrowed.Module() != "tenant" {
		t.Fatalf("the borrowed router mounts in %q and not tenant's namespace", borrowed.Module())
	}
	if token := borrowed.SystemToken(); token != nil {
		t.Fatal("a router bound to another module's namespace handed out a credential")
	}
	said := refused(t, api)
	for _, want := range []string{"billing", "tenant"} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal does not name %q, so it cannot say which two modules it is about: %s", want, said)
		}
	}
	// The borrow costs the module nothing it had: its own router still answers,
	// because the credential belongs to the module and not to the page.
	if own := api.Surfaces("billing").App.SystemToken(); own == nil || own.Module() != "billing" {
		t.Fatalf("the module's own router stopped answering for its credential: %v", own)
	}
}

// refused is what this composition reports at boot, by the call kit/app makes
// before it serves.
func refused(t *testing.T, api *httpx.API) string {
	t.Helper()
	err := api.ValidateDeclarations()
	if err == nil {
		t.Fatal("the composition answered that it may serve")
	}
	return err.Error()
}
