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
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
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
	// OwnWorld builds a committed world with seed and then runs body as the tenant
	// seed created. A self-only command — the one where no id names the subject —
	// takes a tenant transaction, which is a second transaction on a second
	// connection, and a second transaction cannot act on a tenant this one wrote
	// and never committed. So the world such a command needs is a world of its own,
	// and only the harness that opens a connection can build it. Required by the
	// cases in ownCases, which fail rather than skip without it.
	OwnWorld func(seed func(context.Context, db.Tx[db.System]) (*contracts.Tenant, error), body func(Own) error) error
	// LocaleDoor opens or closes the door `SetOwnLocale` rechecks inside its own
	// transaction: false is somebody inside their own tenant who does not hold
	// locale_manage. Both harnesses wire it because both decide it — the service
	// asks the authorizer composition wired, the fake answers from the knob — and a
	// guard tested against one implementation is a guard the other can skip quietly.
	LocaleDoor func(allowed bool)
}

// Own is one case's world seen from inside the tenant: the transaction a request
// to this tenant's own host would run in, and the events that world has published,
// reported the way Fixture.Published reports them. A case about a self-only command
// reads these and not the fixture's because the two worlds are two transactions: the
// declaration and its trail belong to the one that wrote them.
type Own struct {
	Ctx       context.Context
	Tx        db.Tx[db.Tenant]
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
	all := cases()
	maps.Copy(all, ownCases())
	for name, run := range all {
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
		"an operator declaration read at a superseded revision is refused":              refuseSupersededLocale,
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

// ownCases is the tenant's own declaration of its languages: the only command in
// this contract whose subject is the transaction it is given. It is in the shared
// suite rather than only in the module's own Postgres tests because both
// implementations decide it — the service rechecks the grant, locks the one row
// row-level security leaves it, counts the revision and refuses a stale one, the
// fake keeps a map and copies each rule — and a fake that drifted would let every
// module that tests against it accept a declaration the shipped service refuses.
func ownCases() map[string]func(*testing.T, Fixture) {
	// own builds the world and then speaks as the tenant inside it. Two doors have
	// to be wired before any decision below means anything: the committed tenant to
	// be inside and an answer at the grant the command rechecks. A harness that
	// wired neither would run these cases against no world at all, and its
	// refusals would read as decisions.
	own := func(t *testing.T, f Fixture, body func(*testing.T, Fixture, Own)) {
		t.Helper()
		if f.OwnWorld == nil || f.LocaleDoor == nil {
			t.Fatal("this harness wired neither Fixture.OwnWorld nor Fixture.LocaleDoor, " +
				"so the tenant's own language command is untested against this implementation")
		}
		f.LocaleDoor(true)
		err := f.OwnWorld(func(ctx context.Context, tx db.Tx[db.System]) (*contracts.Tenant, error) {
			return f.Service.Create(ctx, tx, acme())
		}, func(w Own) error {
			body(t, f, w)
			return nil
		})
		if err != nil {
			t.Fatalf("acting as the tenant: %v", err)
		}
	}
	// held is the declaration as the tenant's own transaction reads it — the read a
	// refusal is checked against, because "nothing was written" is a claim about the
	// row, not about what the refused call happened to return.
	held := func(t *testing.T, f Fixture, w Own) contracts.Locale {
		t.Helper()
		got, err := f.Service.OwnLocale(w.Ctx, w.Tx)
		if err != nil {
			t.Fatalf("OwnLocale: %v", err)
		}
		return got
	}
	return map[string]func(*testing.T, Fixture){
		"a tenant declares the languages it is served in": func(t *testing.T, f Fixture) {
			own(t, f, func(t *testing.T, f Fixture, w Own) {
				born := held(t, f, w)
				got, err := f.Service.SetOwnLocale(w.Ctx, w.Tx, born.Revision, contracts.SetLocale{
					Default: "pt-PT", Supported: []string{"en", "PT-pt", "pt-PT"},
				})
				if err != nil {
					t.Fatalf("SetOwnLocale: %v", err)
				}
				if got.DefaultLocale != "pt-PT" || !slices.Equal(got.Locales, []string{"en"}) {
					t.Errorf("the tenant is served in %q with %v, want pt-PT with en behind it, spelled once", got.DefaultLocale, got.Locales)
				}
				// The count is what the next declaration is compared against, and the
				// command that made the change is the one that moves it.
				if got.LocaleRevision != born.Revision+1 {
					t.Errorf("the revision is %d after one accepted declaration, want %d", got.LocaleRevision, born.Revision+1)
				}
				if after := held(t, f, w); after.Default != "pt-PT" || !slices.Equal(after.Supported, []string{"en"}) || after.Revision != born.Revision+1 {
					t.Errorf("the tenant's own transaction reads %+v, want the declaration it just made", after)
				}
				if n := len(ownDeclarations(w)); n != 1 {
					t.Errorf("the tenant's transaction published %d locale_set events, want the one its declaration is", n)
				}
			})
		},

		"the declaration the tenant already holds changes nothing and says nothing": func(t *testing.T, f Fixture) {
			own(t, f, func(t *testing.T, f Fixture, w Own) {
				in := contracts.SetLocale{Default: "pt-PT", Supported: []string{"en"}}
				first, err := f.Service.SetOwnLocale(w.Ctx, w.Tx, held(t, f, w).Revision, in)
				if err != nil {
					t.Fatalf("SetOwnLocale: %v", err)
				}
				// The same pair again, brought at the revision the first declaration was
				// read at: an operator who retried the request that already landed changed
				// nothing, so nothing is written, nothing is counted and nothing is said —
				// however old the revision the retry carries.
				again, err := f.Service.SetOwnLocale(w.Ctx, w.Tx, first.LocaleRevision-1, in)
				if err != nil {
					t.Fatalf("SetOwnLocale again: %v", err)
				}
				if again.LocaleRevision != first.LocaleRevision {
					t.Errorf("the repeat moved the revision to %d, where it was %d", again.LocaleRevision, first.LocaleRevision)
				}
				if n := len(ownDeclarations(w)); n != 1 {
					t.Errorf("the tenant's transaction published %d locale_set events, want the one its declaration is", n)
				}
			})
		},

		"a declaration read at an old revision is refused and writes nothing": func(t *testing.T, f Fixture) {
			own(t, f, func(t *testing.T, f Fixture, w Own) {
				born := held(t, f, w)
				if _, err := f.Service.SetOwnLocale(w.Ctx, w.Tx, born.Revision,
					contracts.SetLocale{Default: "pt-PT", Supported: []string{"en"}}); err != nil {
					t.Fatalf("SetOwnLocale: %v", err)
				}
				// Somebody else declared in the meantime, and this form still says it was
				// read before that. Winning the row would erase the other declaration, so
				// the stale one loses — and loses without a trace, the same as every other
				// refusal.
				_, err := f.Service.SetOwnLocale(w.Ctx, w.Tx, born.Revision,
					contracts.SetLocale{Default: "en", Supported: []string{"pt-PT"}})
				if !errors.Is(err, crud.ErrConflict) {
					t.Errorf("a declaration read at revision %d = %v, want ErrConflict", born.Revision, err)
				}
				after := held(t, f, w)
				if after.Default != "pt-PT" || !slices.Equal(after.Supported, []string{"en"}) || after.Revision != born.Revision+1 {
					t.Errorf("the refused declaration left %+v behind; a refusal writes nothing", after)
				}
				if n := len(ownDeclarations(w)); n != 1 {
					t.Errorf("the tenant's transaction published %d locale_set events, want the one its declaration is", n)
				}
			})
		},

		"somebody inside the tenant without the grant to manage its languages is refused": func(t *testing.T, f Fixture) {
			own(t, f, func(t *testing.T, f Fixture, w Own) {
				born := held(t, f, w)
				f.LocaleDoor(false)
				row, err := f.Service.SetOwnLocale(w.Ctx, w.Tx, born.Revision, contracts.SetLocale{Default: "pt-PT"})
				if err == nil {
					t.Error("a caller with no locale_manage wrote the tenant's languages")
				}
				if row != nil {
					t.Errorf("the refusal returned a tenant row %+v; a refusal answers with nothing", row)
				}
				// The shape of the refusal — which problem document, which code — belongs
				// to the door that answers it, and is held there. What this suite holds
				// both implementations to is the decision and the untouched row: a fake
				// that let this caller through would pass every module that tests against
				// it a declaration the shipped service refuses.
				if after := held(t, f, w); after.Default != born.Default || !slices.Equal(after.Supported, born.Supported) || after.Revision != born.Revision {
					t.Errorf("the refusal left %+v, where the tenant was %+v", after, born)
				}
				if n := len(ownDeclarations(w)); n != 0 {
					t.Errorf("a refused declaration published %d locale_set events; a refusal says nothing", n)
				}
			})
		},
	}
}

// ownDeclarations counts the language events in the tenant's own world, so a case
// reads the fact it is about rather than the tenant's creation, which the world was
// built with. Both harnesses report their whole list — the real one reads its own
// transaction's outbox rows, the fake its own record — and the same filter decides
// both, which is the point of a shared suite.
func ownDeclarations(w Own) []string {
	if w.Published == nil {
		return nil
	}
	var got []string
	for _, name := range w.Published() {
		if name == contracts.EventLocaleSet {
			got = append(got, name)
		}
	}
	return got
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
