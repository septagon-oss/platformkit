package pkit_test

// App.Describe answers the resolution Validate answers and Explain prints, as
// values. What these cases judge is not whether the fields are filled — a
// struct that marshals proves nothing — but the four claims the view is made of:
// it says the provider the composition actually picked (including what Choose
// settled over whom), it says the same bytes for the same composition every
// time, a composition that does not resolve describes no composition at all, and
// nothing but names travels in it — no configuration value, no deployment input,
// no credential, no tenant row.
//
// Every case is DB-free and opens nothing: the resolution reads only a
// Deployment's Environment, Inputs and Config, which is also what C14 below
// pins. The fixtures are the same ones the resolver's own cases run over, and
// the code that produces the view is the code the reference application reaches
// through Plan, so a case that passes here judges the answer a client gets.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/admin"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/audit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/blog"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/cart"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/collectibles"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/design"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/homepage"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/payment"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/paypal"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/shop"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/stripe"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/user"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/wishlist"
)

const schema = "platformkit.composition.v1"

// described answers the view and its bytes, so a case can read both the fields
// and the encoding a consumer of the file sees. The document is non-nil on the
// refusal path too, which is part of the contract and checked here once.
func described(t *testing.T, app *pkit.App, d pkit.Deployment) (*pkit.Description, string, error) {
	t.Helper()
	doc, err := app.Describe(d)
	if doc == nil {
		t.Fatalf("Describe answered no document at all (err %v)", err)
	}
	out, merr := json.MarshalIndent(doc, "", "  ")
	if merr != nil {
		t.Fatalf("the document cannot be encoded: %v", merr)
	}
	return doc, string(out), err
}

// describedOK describes a composition that is expected to resolve.
func describedOK(t *testing.T, app *pkit.App, d pkit.Deployment) (*pkit.Description, string) {
	t.Helper()
	doc, out, err := described(t, app, d)
	if err != nil {
		t.Fatalf("Describe refused a composition Validate and Explain answer: %v\n%s", err, out)
	}
	if !doc.Resolved || len(doc.Problems) != 0 {
		t.Fatalf("resolved=%v with %d problems, want true and none:\n%s", doc.Resolved, len(doc.Problems), out)
	}
	return doc, out
}

func moduleNamed(doc *pkit.Description, name string) (*pkit.DescribedModule, error) {
	for i, m := range doc.Modules {
		if m.Name == name {
			return &doc.Modules[i], nil
		}
	}
	return nil, fmt.Errorf("no module %q in the description", name)
}

func TestDescribeNamesTheProviderTheCompositionPicked(t *testing.T) {
	contested := pkit.NewApp("collect").Use(user.Module, wishlist.Module, stripe.Module, paypal.Module)

	doc, out, err := described(t, contested, dev)
	if err == nil {
		t.Fatal("Describe answered a composition whose provider nobody chose")
	}
	if doc.Resolved {
		t.Fatalf("two providers of one contract described as settled:\n%s", out)
	}
	if len(doc.Modules) != 0 {
		t.Errorf("a composition that does not resolve described %d modules:\n%s", len(doc.Modules), out)
	}
	if len(doc.Problems) != 1 || doc.Problems[0].Cause != "Choose" {
		t.Fatalf("the ambiguity is not the one problem the view reports: %+v", doc.Problems)
	}
	// The sentence is Validate's own, prefix and all, so a caller prints what the
	// resolver said rather than a second message about it.
	want := strings.TrimPrefix(contested.Validate(dev).Error(), "pkit: collect: Choose: ")
	if doc.Problems[0].Sentence != want {
		t.Errorf("the problem says %q, want Validate's %q", doc.Problems[0].Sentence, want)
	}

	picked := pkit.NewApp("collect").Use(user.Module, wishlist.Module, stripe.Module, paypal.Module).
		Choose(stripe.Module)
	doc, out = describedOK(t, picked, dev)
	if doc.Schema != schema || doc.App != "collect" || doc.Environment != pkit.Development {
		t.Errorf("the document does not name its format, app and environment: %+v", doc)
	}
	if len(doc.Choices) != 1 {
		t.Fatalf("Choose settled nothing the view can see: %+v", doc.Choices)
	}
	c := doc.Choices[0]
	if c.Contract != "paymentcontracts.Provider" || c.Picked != "stripe" || strings.Join(c.PassedOver, ",") != "paypal" {
		t.Errorf("the choice is described as %+v, want paymentcontracts.Provider picked stripe over paypal", c)
	}
	if _, err := moduleNamed(doc, "paypal"); err == nil {
		t.Errorf("the module Choose did not pick is still in the composition it is not part of:\n%s", out)
	}
	wish, err := moduleNamed(doc, "wishlist")
	if err != nil {
		t.Fatal(err)
	}
	if len(wish.Uses) != 1 || wish.Uses[0].Contract != "paymentcontracts.Provider" || wish.Uses[0].From != "stripe" {
		t.Errorf("wishlist's optional need is answered by %+v, want stripe", wish.Uses)
	}
	// The same decision, in the words the composition file carries.
	text, err := picked.Explain(dev)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "collect chose stripe.Module over paypal.Module for paymentcontracts.Provider.") {
		t.Errorf("Explain does not agree with the view about who was chosen:\n%s", text)
	}
}

func TestDescribeRefusesWhatValidateRefuses(t *testing.T) {
	for _, tc := range []struct {
		what  string
		app   *pkit.App
		once  pkit.Deployment
		cause string
		said  string
	}{
		{"a missing provider", pkit.NewApp("collect").Use(collectibles.Module), dev, "Use",
			"collectibles needs cartcontracts.Service: add cart.Module to collect"},
		{"a cycle", pkit.NewApp("collect").Use(cart.Module, collectibles.Module), dev, "Use",
			"cart → collectibles → cart is a cycle"},
		{"two modules after everything", pkit.NewApp("collect").Use(admin.Module, audit.Module), dev, "Use",
			"admin and audit both ask to run after everything: only one module may"},
		{"a deployment that names no environment", pkit.NewApp("collect").Use(user.Module),
			pkit.Deployment{}, "Deployment",
			`"" is not an environment: name development, staging or production`},
	} {
		doc, out, err := described(t, tc.app, tc.once)
		if err == nil {
			t.Errorf("%s: Describe answered without a refusal:\n%s", tc.what, out)
			continue
		}
		if doc.Resolved {
			t.Errorf("%s: a refusal says resolved=true:\n%s", tc.what, out)
		}
		if len(doc.Modules) != 0 {
			t.Errorf("%s: the refusal still carries %d modules — a partial graph is a misleading one:\n%s",
				tc.what, len(doc.Modules), out)
		}
		if len(doc.Roles) != 0 || len(doc.Choices) != 0 {
			t.Errorf("%s: the refusal carries roles or choices: %+v", tc.what, doc)
		}
		if len(doc.Problems) == 0 || doc.Problems[0].Cause != tc.cause {
			t.Errorf("%s: the problems are %+v, want the first caused by %s", tc.what, doc.Problems, tc.cause)
			continue
		}
		said := strings.TrimPrefix(tc.app.Validate(tc.once).Error(), "pkit: collect: "+tc.cause+": ")
		if doc.Problems[0].Sentence != said || said != tc.said {
			t.Errorf("%s: the problem says %q, want Validate's %q", tc.what, doc.Problems[0].Sentence, said)
		}
		// The refusal is a machine document too: a caller that reads only the
		// bytes sees an empty composition and a cause, never a graph.
		for _, want := range []string{`"resolved": false`, `"modules": []`, `"cause": "` + tc.cause + `"`} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: the refused document is missing %s:\n%s", tc.what, want, out)
			}
		}
	}
}

func TestDescribeNamesWhoTakesAContributionAndWhoDoesNot(t *testing.T) {
	doc, out := describedOK(t, pkit.NewApp("collect").Use(user.Module, wishlist.Module, cart.Module), dev)
	wish, err := moduleNamed(doc, "wishlist")
	if err != nil {
		t.Fatal(err)
	}
	if len(wish.Contributes) != 1 || wish.Contributes[0].Contract != "cartcontracts.Extension" ||
		strings.Join(wish.Contributes[0].To, ",") != "cart" {
		t.Errorf("wishlist contributes %+v, want one cartcontracts.Extension to cart", wish.Contributes)
	}
	c, err := moduleNamed(doc, "cart")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Takes) != 1 || c.Takes[0].Contract != "cartcontracts.Extension" ||
		strings.Join(c.Takes[0].From, ",") != "wishlist" {
		t.Errorf("cart takes %+v, want every cartcontracts.Extension from wishlist", c.Takes)
	}

	// A contribution nobody takes is a note, not a refusal (decision 0074), and
	// "nobody" is an answered question: the list is there and empty. An omitempty
	// on `to` would make "nobody takes it" and "not asked" one state.
	doc, out = describedOK(t, pkit.NewApp("collect").Use(blog.Module), dev)
	b, err := moduleNamed(doc, "blog")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Contributes) != 1 || len(b.Contributes[0].To) != 0 {
		t.Fatalf("blog contributes %+v, want one landing with nobody taking it", b.Contributes)
	}
	if !strings.Contains(out, `"to": []`) {
		t.Errorf("an unanswered contribution is absent rather than empty:\n%s", out)
	}
}

func TestDescribeAnswersTheSameBytesTwice(t *testing.T) {
	app := pkit.NewApp("collect").Use(user.Module, wishlist.Module, cart.Module, shop.Module, homepage.Module)
	first := describeOnce(t, app)
	second := describeOnce(t, app)
	if first != second {
		t.Errorf("one app described twice is two documents:\n%s\n---\n%s", first, second)
	}
}

// TestDescribeOfTwoIdenticalSentencesIsTheSameBytes is the case that catches a
// projection that ranged a resolver map: Go randomises map order per run, so two
// calls in one process can agree while two processes disagree. The committed
// bytes are the other process.
func TestDescribeOfTwoIdenticalSentencesIsTheSameBytes(t *testing.T) {
	sentence := func() *pkit.App {
		return pkit.NewApp("collect").
			Use(user.Module, wishlist.Module, cart.Module, shop.Module, homepage.Module).
			Roles(pkit.Role{Name: "member", Grants: []string{"task.read"}})
	}
	first := describeOnce(t, sentence())
	second := describeOnce(t, sentence())
	if first != second {
		t.Errorf("two spellings of one composition are two documents:\n%s\n---\n%s", first, second)
	}

	path := filepath.Join("testdata", "composition-development.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, append([]byte(first), '\n'), 0o644); err != nil {
			t.Fatalf("write the golden: %v", err)
		}
	}
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s is not committed (run UPDATE_GOLDEN=1 go test ./pkit -run TestDescribeOfTwoIdentical): %v", path, err)
	}
	if got := strings.TrimSuffix(string(committed), "\n"); got != first {
		t.Errorf("%s is not what the composition describes today; regenerate it the same way\n--- committed ---\n%s\n--- described now ---\n%s", path, committed, first)
	}
}

// describeOnce encodes a composition the way a caller would write it to a file.
func describeOnce(t *testing.T, app *pkit.App) string {
	t.Helper()
	_, out := describedOK(t, app, dev)
	return out
}

func TestDescribeListsModulesInTheBuildOrder(t *testing.T) {
	doc, _ := describedOK(t, pkit.NewApp("collect").Use(admin.Module, user.Module, cart.Module), dev)
	var got []string
	for _, m := range doc.Modules {
		got = append(got, m.Name)
	}
	if strings.Join(got, ",") != "user,cart,admin" {
		t.Errorf("the modules are described in order %v, want the order the composition builds them", got)
	}
	last, err := moduleNamed(doc, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !last.RunsLast {
		t.Error("the After(Everything) module is not marked as the one that runs last")
	}
	for _, m := range doc.Modules {
		if m.Name != "admin" && m.RunsLast {
			t.Errorf("%s is marked runsLast; only one module may be", m.Name)
		}
	}
}

func TestDescribeCarriesRolesAsRecorded(t *testing.T) {
	doc, _ := describedOK(t, pkit.NewApp("collect").Use(user.Module).
		Roles(pkit.Role{Name: "member", Grants: []string{"task.read"}}, pkit.Role{Name: "watcher"}), dev)
	if len(doc.Roles) != 2 {
		t.Fatalf("the view carries %d roles, want the two the app recorded", len(doc.Roles))
	}
	if doc.Roles[0].Name != "member" || strings.Join(doc.Roles[0].Grants, ",") != "task.read" {
		t.Errorf("the first role is %+v, want member holding task.read", doc.Roles[0])
	}
	if doc.Roles[1].Name != "watcher" || doc.Roles[1].Grants != nil {
		t.Errorf("the second role is %+v, want watcher holding nothing", doc.Roles[1])
	}
}

func TestDescribeNamesTheImplementationAndNeverItsInputs(t *testing.T) {
	secrets := map[string]string{"stripe.key": "sk_live_ZZZ", "stripe.webhook.secret": "whsec_ZZZ"}
	doc, out := describedOK(t, pkit.NewApp("collect").Use(payment.Module), production(secrets))
	m, err := moduleNamed(doc, "payment")
	if err != nil {
		t.Fatal(err)
	}
	if m.Implementation == nil {
		t.Fatalf("the FromDeployment module is described with no implementation:\n%s", out)
	}
	impl := *m.Implementation
	if impl.Name != "stripe" || impl.Simulated || strings.Join(impl.Inputs, ",") != "stripe.key,stripe.webhook.secret" {
		t.Errorf("the picked implementation is %+v, want stripe, real, reading those two input names", impl)
	}
	for _, secret := range []string{"sk_live_ZZZ", "whsec_ZZZ"} {
		if strings.Contains(out, secret) {
			t.Errorf("the deployment's input %q reached the document", secret)
		}
	}

	// The same composition in development runs on the simulated implementation,
	// and says so, because that is the decision the environment made.
	doc, out = describedOK(t, pkit.NewApp("collect").Use(payment.Module), dev)
	m, err = moduleNamed(doc, "payment")
	if err != nil {
		t.Fatal(err)
	}
	if m.Implementation == nil || m.Implementation.Name != "manual" || !m.Implementation.Simulated {
		t.Errorf("in development the implementation is %+v, want manual and simulated", m.Implementation)
	}
	if !m.Implementation.Simulated || strings.Contains(out, `"inputs"`) {
		t.Errorf("the simulated implementation carries inputs it does not read:\n%s", out)
	}
}

func TestDescribeCarriesSectionNamesAndNoValues(t *testing.T) {
	var kept config.Audit
	cfg := config.Config{Audit: config.Audit{RetentionDays: 90}}
	doc, out := describedOK(t, pkit.NewApp("collect").Use(auditReader(&kept), silent("shop")), withAudit(cfg))

	m, err := moduleNamed(doc, "audit")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(m.Reads, ",") != "config.Audit" {
		t.Errorf("audit reads %v, want the section's name", m.Reads)
	}
	if other, err := moduleNamed(doc, "shop"); err != nil {
		t.Fatal(err)
	} else if other.Reads != nil {
		t.Errorf("shop is credited with reading %v, and it read nothing", other.Reads)
	}
	if !strings.Contains(out, "config.Audit") {
		t.Fatalf("the section the build read is nowhere in the document:\n%s", out)
	}
	// The name is the fact; the value the deployment supplied is not in it.
	for _, want := range []string{"90", "RetentionDays", "retention_days"} {
		if strings.Contains(out, want) {
			t.Errorf("the document carries %q, which is a configuration value and not a name:\n%s", want, out)
		}
	}
}

// TestDescribeDoesNotMarryTheSameSectionTwice pins the dedupe where it lives:
// one module asking for one section twice asked for one thing, and both the
// document and the composition file say it once.
func TestDescribeDoesNotMarryTheSameSectionTwice(t *testing.T) {
	twice := pkit.NewModule("audit", func(w *pkit.Wiring) (module.Module, error) {
		_ = pkit.Config(w, func(c config.Config) config.Audit { return c.Audit })
		_ = pkit.Config(w, func(c config.Config) config.Audit { return c.Audit })
		return module.Module{Name: "audit"}, nil
	})
	app := pkit.NewApp("collect").Use(twice)
	doc, out := describedOK(t, app, withAudit(config.Config{Audit: config.Audit{RetentionDays: 90}}))
	m, err := moduleNamed(doc, "audit")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(m.Reads, ",") != "config.Audit" {
		t.Errorf("the section is read as %v, want it named once", m.Reads)
	}
	if n := strings.Count(out, "config.Audit"); n != 1 {
		t.Errorf("the document names the section %d times, want once:\n%s", n, out)
	}
	text, err := app.Explain(withAudit(config.Config{}))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(text, "pkit: audit.Module reads config.Audit.\n"); n != 1 {
		t.Errorf("Explain printed the read %d times, want once, as the document does:\n%s", n, text)
	}
}

// TestDescribeCarriesNoSecretShapedValue is the exclusion rule as a check: a
// configuration the whole document was built beside, with a sentinel in every
// field the human composition file is willing to name.
func TestDescribeCarriesNoSecretShapedValue(t *testing.T) {
	cfg := config.Config{}
	cfg.Database.URL = "postgres://app:SENTINEL-dsn@db.internal:5432/platformkit?sslmode=require"
	cfg.Database.MigrateURL = "postgres://postgres:SENTINEL-migrate@db.internal:5432/platformkit"
	cfg.Auth.OIDC.ClientSecret = "SENTINEL-client-secret"
	cfg.Server.InstallationHost = "SENTINEL-installation.platformkit.example"
	cfg.NATS.URL = "nats://user:SENTINEL-nats@broker.internal:4222"

	_, out := describedOK(t, pkit.NewApp("collect").Use(
		auditReader(&config.Audit{}), user.Module, wishlist.Module, cart.Module), withAudit(cfg))
	for _, sentinel := range []string{"SENTINEL-dsn", "SENTINEL-migrate", "SENTINEL-client-secret",
		"SENTINEL-installation", "SENTINEL-nats", "db.internal", "broker.internal"} {
		if strings.Contains(out, sentinel) {
			t.Errorf("the document carries %q: a machine-readable file is a wider surface than a markdown one a reviewer reads\n%s",
				sentinel, out)
		}
	}
}

// TestDescribeSeparatesDeclarationsFromRuntime keeps the view to what the
// resolver owns. A routes key here would be a fabrication: pkit does not mount
// routes, does not answer the kernel's port questions from the resolution, hosts
// no tenant and observes no request.
func TestDescribeSeparatesDeclarationsFromRuntime(t *testing.T) {
	_, out := describedOK(t, pkit.NewApp("collect").Use(user.Module, wishlist.Module, cart.Module, payment.Module), dev)
	forbidden := regexp.MustCompile(`(?i)route|path|url|host|port|server|listen|tenant|secret|dsn|surface|manifest|permission|emits`)
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("the document is not JSON: %v", err)
	}
	check := func(where string, keys []string) {
		for _, k := range keys {
			if forbidden.MatchString(k) {
				t.Errorf("%s carries the key %q, which is not the resolver's to say", where, k)
			}
		}
	}
	var top []string
	for k := range doc {
		top = append(top, k)
	}
	check("the top level", top)
	for i, raw := range doc["modules"].([]any) {
		var keys []string
		for k := range raw.(map[string]any) {
			keys = append(keys, k)
		}
		check(fmt.Sprintf("modules[%d]", i), keys)
	}
	// The keys that are there, exactly, so a case that fails open cannot pass by
	// describing nothing.
	if strings.Join(top, ",") == "" || len(doc["modules"].([]any)) == 0 {
		t.Fatalf("the document is too empty to prove anything:\n%s", out)
	}
}

// TestDescribeRunsTheModulesItDescribes pins in a test what Describe's doc
// comment says in words: the modules' own trusted build functions run during
// inspection, because what a module reads is only observable from inside its own
// build. This is a call into your own composition, not a sandbox around
// somebody else's.
func TestDescribeRunsTheModulesItDescribes(t *testing.T) {
	runs := 0
	probe := pkit.NewModule("probe", func(w *pkit.Wiring) (module.Module, error) {
		runs++
		return module.Module{Name: "probe"}, nil
	})
	if _, out := describedOK(t, pkit.NewApp("collect").Use(probe), dev); runs != 1 {
		t.Errorf("the module's build ran %d times during one Describe, want once:\n%s", runs, out)
	}
}

// TestDescribeNeedsNoTransportAndNoCredential is the acceptance line that
// inspection is not deployment: the resolution reads a Deployment's environment,
// inputs and configuration and nothing else, so an inspection needs no broker
// constructor, no cache constructor and no database URL. A view moved onto Plan
// or newEngine would refuse this deployment, which is why it is pinned.
func TestDescribeNeedsNoTransportAndNoCredential(t *testing.T) {
	bare := pkit.Deployment{Environment: pkit.Production, Inputs: map[string]string{
		"stripe.key": "sk_live_1", "stripe.webhook.secret": "whsec_1",
	}}
	if _, out := describedOK(t, pkit.NewApp("collect").Use(user.Module, wishlist.Module, payment.Module), bare); len(out) == 0 {
		t.Fatal("nothing was described")
	}
}

// TestDescribeAgreesWithExplainOverEveryFixtureComposition is the agreement over
// many compositions rather than one: for every subset of these fixture modules,
// what the view calls resolved is what Validate answers, the module list is the
// build order Explain counts, and every supplier the view names is the one the
// sentence names. It is the sweep of
// every_accepted_composition_builds_what_use_named_test.go, read through both
// encodings instead of one.
func TestDescribeAgreesWithExplainOverEveryFixtureComposition(t *testing.T) {
	fixtures := []*pkit.Module{
		user.Module, wishlist.Module, cart.Module, collectibles.Module, blog.Module,
		shop.Module, homepage.Module, payment.Module, design.Module, admin.Module, stripe.Module,
	}
	seen := map[string]int{}
	for mask := 0; mask < 1<<len(fixtures); mask++ {
		var ms []*pkit.Module
		for i, m := range fixtures {
			if mask>>i&1 == 1 {
				ms = append(ms, m)
			}
		}
		if len(ms) == 0 {
			continue
		}
		app := pkit.NewApp("collect").Use(ms...)
		doc, out, err := described(t, app, dev)
		text, explainErr := app.Explain(dev)
		if (err == nil) != doc.Resolved || (err == nil) != (explainErr == nil) {
			t.Fatalf("the two encodings disagree about whether %v resolves: describe %v, explain %v\n%s", names(ms), err, explainErr, out)
		}
		if err != nil {
			if len(doc.Modules) != 0 {
				t.Fatalf("%v is refused and still described %d modules:\n%s", names(ms), len(doc.Modules), out)
			}
			continue
		}
		if n := len(doc.Modules); !strings.Contains(text, fmt.Sprintf("builds %d %s.", n, moduleWord(n))) {
			t.Fatalf("%v: Explain counts a different composition than the view describes (%d modules)\n%s\n%s",
				names(ms), n, out, text)
		}
		for _, m := range doc.Modules {
			for _, need := range append(append([]pkit.DescribedNeed{}, m.Needs...), m.Uses...) {
				if need.From == "" {
					// An optional need the app composes no provider for is the one
					// answer the text spells as a sentence rather than a name.
					want := fmt.Sprintf("pkit: %s.Module could use %s; collect composes no provider, so it will not.",
						m.Name, need.Contract)
					if !strings.Contains(text, want) {
						t.Fatalf("%v: the view says %s has no provider for %s, which the text does not say\n%s\n%s",
							names(ms), m.Name, need.Contract, out, text)
					}
					continue
				}
				seen[need.From]++
				verb := "needs"
				if isUseOf(m, need.Contract) {
					verb = "uses"
				}
				sentence := fmt.Sprintf("pkit: %s.Module %s %s from %s.Module.", m.Name, verb, need.Contract, need.From)
				if !strings.Contains(text, sentence) {
					t.Fatalf("%v: the view says %s %s %s from %s, which the text does not say\n%s\n%s",
						names(ms), m.Name, verb, need.Contract, need.From, out, text)
				}
			}
		}
	}
	if len(seen) < 5 {
		t.Fatalf("the sweep named %d suppliers, which is too few to have checked anything", len(seen))
	}
}

func moduleWord(n int) string {
	if n == 1 {
		return "module"
	}
	return "modules"
}

func names(ms []*pkit.Module) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Name())
	}
	return out
}

// isUseOf asks which verb the composition file should use for one contract this
// module declared: Needs says "needs", Optional says "uses".
func isUseOf(m pkit.DescribedModule, contract string) bool {
	for _, u := range m.Uses {
		if u.Contract == contract {
			return true
		}
	}
	return false
}

// TestDescribedModuleFieldsAreOnlyWhatTheResolverOwns is the projection read the
// other way: every exported key of the document must be filled by something in
// the composition, so a field that decays into decoration is refused here rather
// than discovered by a consumer that trusted it.
func TestDescribedModuleFieldsAreOnlyWhatTheResolverOwns(t *testing.T) {
	doc, _ := describedOK(t, pkit.NewApp("collect").Use(
		user.Module, wishlist.Module, cart.Module, shop.Module, homepage.Module, payment.Module, design.Module, admin.Module,
	).Roles(pkit.Role{Name: "member"}), dev)
	for _, k := range []string{"Schema", "App", "Environment", "Resolved", "Modules", "Choices", "Roles", "Problems"} {
		if _, ok := reflect.TypeOf(*doc).FieldByName(k); !ok {
			t.Errorf("Description lost the field %s, which the format version names", k)
		}
	}
	for _, k := range []string{"Name", "Provides", "Needs", "Uses", "Takes", "Contributes", "After", "Reads", "RunsLast", "Implementation"} {
		if _, ok := reflect.TypeOf(doc.Modules[0]).FieldByName(k); !ok {
			t.Errorf("DescribedModule lost the field %s, which a consumer reads", k)
		}
	}
}
