// Package tenanttest is the conformance suite for contracts.Service, and a fake
// that passes it.
//
// An interface is justified by a passing fake and not by a second production
// implementation (AGENTS.md rule 8). RunService is the specification of the
// control plane written as executable cases; the real service and the fake both
// run it, so "the fake behaves like the real thing" is a test result.
package tenanttest

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// Fixture is one case's world: a Service, the cross-tenant transaction its
// commands take, and a way to see what was published. The transaction is the
// real thing for the real service and the zero value for the fake, which never
// looks at it.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.System]
	Service contracts.Service
	// Published is the names of the events published so far, in order. The
	// fake returns its own slice; the real harness reads the outbox. It is what
	// makes "an idempotent command says nothing" a thing the suite can check,
	// rather than a claim the returned entity cannot support.
	Published func() []string
	// PublishedScopes and PublishedTraces answer the two questions a verb that
	// audits both sides has to answer: which tenant's trail each row landed in,
	// and which request both rows name. They are in the same order as Published.
	PublishedScopes func() []uuid.UUID
	PublishedTraces func() []string
	// Operator is the installation's own tenant — the row Bootstrap creates and
	// the scope every lifecycle verb mirrors its audit row into. The fixture
	// carries one because every verb refuses without it: a control plane that
	// cannot audit both sides of what it did writes neither.
	Operator uuid.UUID
}

// Harness builds one Fixture and calls run with it. It is written this way
// round because the real fixture is a transaction, and a transaction is a scope
// somebody has to close.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	for name, run := range cases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

func acme() contracts.NewTenant {
	return contracts.NewTenant{Slug: "acme", Name: "Acme Corporation", Host: "acme.example.com"}
}

// people is a provider somebody would actually configure: a realm issuer, the
// installation's own client, and the *name* of the variable holding the secret.
func people() contracts.OIDCSettings {
	return contracts.OIDCSettings{
		Issuer: "https://idp.acme.example/realms/people", ClientID: "platformkit",
		SecretRef: "ACME_OIDC_SECRET", RedirectPath: "/oidc/callback",
	}
}

// replaced is the same shape with a different door behind it.
func replaced() contracts.OIDCSettings {
	in := people()
	in.Issuer = "https://login.acme.example"
	return in
}

func cases() map[string]func(*testing.T, Fixture) {
	all := map[string]func(*testing.T, Fixture){
		"create writes the tenant and its first host":   createTenant,
		"create normalises the slug and the host":       normaliseTenant,
		"create refuses a slug that is not a DNS label": refuseInvalidSlug,
		"create refuses a tenant with no host":          requireHost,
		// The two conflicts are two cases and not one, because they cannot
		// share a transaction: a failed write aborts the whole transaction in
		// Postgres, so a caller that means to try something else after a
		// constraint violation needs a new one — which for an HTTP handler
		// means a new request. See kit/crud.Update.
		"a slug is taken once":                                                          reserveSlug,
		"a host is served by one tenant":                                                reserveHost,
		"an unserved host resolves to nothing":                                          resolveUnknownHost,
		"a suspended tenant stops being served":                                         suspendTenant,
		"suspending twice says nothing the second time":                                 repeatSuspension,
		"another host resolves to the same tenant":                                      addHost,
		"the first host is the primary one, and stays it until somebody says otherwise": selectPrimaryHost,
		"set-locale says what the tenant is served in":                                  setLocale,
		"saying the same languages again changes nothing and says nothing":              repeatLocale,
		"a language that is not a tag is refused":                                       refuseInvalidLanguage,
		"a language the installation has no copy for is refused":                        refuseUnavailableLanguage,
		// The provider's four rules, run against the SQL service and the fake by
		// the same table: a mode that is not one of three, half a provider, an
		// issuer that is a sentence, a secret written where its name belongs.
		"set-oidc says which provider a tenant's people sign in against": setOIDC,
		// Every one of these is a write that cannot be exercised: a redirect to a
		// provider that will refuse the code, a door into a room with no roles, a
		// secret in a column that is copied into the audit trail. Each refusal
		// publishes nothing, which is the assertion that makes it a refusal.
		"set-oidc refuses half a provider": refusePartialOIDC,
		// A retry is the same decision reported once. An operator who pressed save
		// twice must not read as two changes in the trail a customer is billed on.
		"the same provider twice changes nothing and says nothing": repeatOIDC,
		"clearing a provider ends it once":                         clearOIDC,
		"an unknown tenant is not found":                           refuseUnknownTenant,
	}
	for name, run := range lifecycleCases() {
		all[name] = run
	}
	return all
}

// names is a tenant list as one line of failure output.
func names(ts []*contracts.Tenant) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Slug+"/"+t.Status)
	}
	return out
}

// others is the customers of a list, the installation's own row left out: the
// fixture bootstraps one, and a case about a customer counts customers.
func others(ts []*contracts.Tenant, operator uuid.UUID) []*contracts.Tenant {
	out := make([]*contracts.Tenant, 0, len(ts))
	for _, t := range ts {
		if t.ID != operator {
			out = append(out, t)
		}
	}
	return out
}

func othersTenants(ts []tenancy.Tenant, operator uuid.UUID) []tenancy.Tenant {
	out := make([]tenancy.Tenant, 0, len(ts))
	for _, t := range ts {
		if t.ID != operator {
			out = append(out, t)
		}
	}
	return out
}

// audited is each verb event beside the operator's mirror of it — the two rows one
// lifecycle command writes, in two trails, from one request.
//
// The three commands that are not lifecycle changes pass through without a mirror:
// which language a tenant is served in, and which provider its people sign in
// against, move no lifecycle fact about the customer. A function rather than a
// constant, so the exception is somewhere a reader is told.
func audited(names ...string) []string {
	out := make([]string, 0, 2*len(names))
	for _, name := range names {
		out = append(out, name)
		if !commandOnly(name) {
			out = append(out, contracts.EventLifecycleRecorded)
		}
	}
	return out
}

// commandOnly names the events a control-plane command publishes in its subject's
// trail and nowhere else. Each is a decision *about* a customer that leaves the
// customer served: the languages, and the identity provider.
func commandOnly(name string) bool {
	return name == contracts.EventLocaleSet ||
		name == contracts.EventOIDCSet || name == contracts.EventOIDCCleared
}

// published asserts the exact sequence of events, which is how an idempotent
// command is held to saying nothing the second time.
func published(t *testing.T, f Fixture, want ...string) {
	t.Helper()
	if f.Published == nil {
		return
	}
	if got := f.Published(); !slices.Equal(got, want) {
		t.Errorf("published %v, want %v", got, want)
	}
}
