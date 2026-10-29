package main

// Each module that crosses tenants holds one credential of its own: the audit
// trail of a cross-tenant transaction names the module that opened it, which is
// only true if the value a module's Routes received was minted for that module.
// The manifests state the need and the four sentences; what they cannot say is
// what the kernel handed out, so this reads the credentials the reference
// composition's own modules took through the door they take them at.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
)

// referenceSurfaces wires the composition's modules onto an API with no
// connection behind it and hands back the API and the manifests it wired, so a
// case can ask what each module was handed while it was being registered.
func referenceSurfaces(t *testing.T) (*httpx.API, []module.Module) {
	t.Helper()
	_, cfg := configure(t)
	c := compose(cfg)
	api, _ := httpx.New(httpx.Options{
		Unwired: true, Tenants: c.testTenants(), Authorize: c.testAuth(),
		Authenticate: c.testAuth().Authenticate, Cache: cache.Memory("pkit"), Log: quiet(),
		Entitle: c.plans, PublicHost: cfg.Server.PublicHost, Installation: cfg.Server.InstallationHost,
		SystemAccess: systemAccessOf(c.modules),
	})
	for _, m := range c.modules {
		if m.Routes != nil {
			m.Routes(api.Surfaces(m.Name))
		}
	}
	return api, c.modules
}

// TestEveryModuleThatCrossesTenantsHoldsACredentialOfItsOwn is the trail's
// subject: each module whose manifest declares cross-tenant work holds a
// credential that names that module and carries its own declared sentence, and
// no two of them hold one value.
func TestEveryModuleThatCrossesTenantsHoldsACredentialOfItsOwn(t *testing.T) {
	api, mods := referenceSurfaces(t)
	held := map[string]string{}
	holders := 0
	for _, m := range mods {
		if !m.SystemAccess.Declared() {
			continue
		}
		holders++
		taken := api.Surfaces(m.Name).App.SystemToken()
		if taken == nil {
			t.Errorf("module %q declared %q and was handed no credential", m.Name, m.SystemAccess.Reason)
			continue
		}
		if taken.Module() != m.Name {
			t.Errorf("module %q holds a credential naming %q", m.Name, taken.Module())
		}
		if taken.Reason() != m.SystemAccess.Reason {
			t.Errorf("module %q's credential carries %q and not the declared %q", m.Name, taken.Reason(), m.SystemAccess.Reason)
		}
		if prior, seen := held[taken.Reason()]; seen {
			t.Errorf("modules %q and %q hold one credential: the audit trail cannot name one of them", prior, m.Name)
		}
		held[taken.Reason()] = m.Name
	}
	if holders == 0 {
		t.Fatal("this composition carries no module that crosses tenants, so the case asks nothing")
	}
}

// TestAModuleOfThisCompositionThatDeclaredNothingIsRefusedItsCredential walks
// the same door with a module of this application that crosses no tenant
// boundary, and asks the question the composition asks before it serves: the
// credential is nil, and the refusal names the module whose manifest is short.
func TestAModuleOfThisCompositionThatDeclaredNothingIsRefusedItsCredential(t *testing.T) {
	api, mods := referenceSurfaces(t)
	stayInside := module.Module{}
	for _, m := range mods {
		if !m.SystemAccess.Declared() {
			stayInside = m
			break
		}
	}
	if stayInside.Name == "" {
		t.Fatal("every module of this composition crosses tenants, so there is nothing to ask")
	}
	if taken := api.Surfaces(stayInside.Name).App.SystemToken(); taken != nil {
		t.Fatalf("module %q was handed a credential named %q without declaring the need", stayInside.Name, taken.Module())
	}
	err := api.ValidateDeclarations()
	if err == nil {
		t.Fatal("the composition answered that it may serve after handing out an undeclared credential")
	}
	for _, want := range []string{stayInside.Name, "SystemAccess"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}
