package pkit_test

// The fixture is decision 0074's rule 3 table composed as apps: every row —
// provides, needs, optional, contributes, After(Everything),
// FromDeployment, Choose and Describes — appears in one of these
// compositions, and every failure sentence the decision names is produced
// here by the composition that deserves it.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/admin"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/audit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/blog"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/cart"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/collectibles"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/design"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/homepage"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/payment"
	paymentcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/payment/contracts"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/paypal"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/reports"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/rewards"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/shipping"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/shop"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/stripe"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/user"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/wishlist"
)

var dev = pkit.Deployment{Environment: pkit.Development}

func production(inputs map[string]string) pkit.Deployment {
	return pkit.Deployment{Environment: pkit.Production, Inputs: inputs}
}

// says fails unless err answers with exactly this sentence.
func says(t *testing.T, err error, sentence string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), sentence) {
		t.Fatalf("error does not say %q:\n%v", sentence, err)
	}
}

// probe reads what the resolver wired, so the composition's answers are
// observable from the test rather than from inside a fixture module.
type probe struct {
	service    cartcontracts.Service
	provider   paymentcontracts.Provider
	extensions []cartcontracts.Extension
}

func probeModule(got *probe) *pkit.Module {
	return pkit.NewModule("probe", func(w *pkit.Wiring) (module.Module, error) {
		got.service = pkit.Get[cartcontracts.Service](w)
		got.provider = pkit.Get[paymentcontracts.Provider](w)
		got.extensions = pkit.All[cartcontracts.Extension](w)
		return module.Module{Name: "probe"}, nil
	},
		pkit.Needs[cartcontracts.Service](),
		pkit.Optional[paymentcontracts.Provider](),
		pkit.Needs[[]cartcontracts.Extension](),
	)
}

func TestHappyCompositionResolvesAndBuilds(t *testing.T) {
	got := &probe{}
	app := pkit.NewApp("collect").Use(
		user.Module, wishlist.Module, cart.Module, shop.Module, homepage.Module,
		payment.Module, design.Module, admin.Module, probeModule(got),
	)
	if err := app.Validate(dev); err != nil {
		t.Fatalf("the honest composition was refused: %v", err)
	}
	if got.service == nil || got.service.Total() != 1 {
		t.Errorf("cart provided %v, want one extension counted", got.service)
	}
	if len(got.extensions) != 1 || got.extensions[0].Module != "person owner with payment" {
		t.Errorf("wishlist contributed %+v, want the paid owner's extension", got.extensions)
	}
	if got.provider == nil {
		t.Fatal("the composed payment provider did not reach the optional need")
	}
	if s := got.provider.Charge(55); s != "manual:55" {
		t.Errorf("development charged through %q, want the simulation", s)
	}
}

func TestMissingProviderNamesTheModuleTheContractAndTheModuleToAdd(t *testing.T) {
	app := pkit.NewApp("collect").Use(collectibles.Module)
	says(t, app.Validate(dev),
		"pkit: collect: Use: collectibles needs cartcontracts.Service: add cart.Module to collect")
	if n := strings.Count(app.Validate(dev).Error(), "\n"); n != 0 {
		t.Errorf("one missing provider answered %d sentences, want the one: %v", n+1, app.Validate(dev))
	}
	says(t, pkit.NewApp("collect").Use(wishlist.Module).Validate(dev),
		"pkit: collect: Use: wishlist needs usercontracts.Service: add user.Module to collect")
}

func TestTwoProvidersOfOneContractAskForChoose(t *testing.T) {
	app := pkit.NewApp("collect").Use(user.Module, wishlist.Module, stripe.Module, paypal.Module)
	says(t, app.Validate(dev),
		"pkit: collect: Choose: paymentcontracts.Provider has stripe.Module and paypal.Module: Choose one in collect")

	picked := pkit.NewApp("collect").Use(user.Module, wishlist.Module, stripe.Module, paypal.Module).
		Choose(stripe.Module)
	if err := picked.Validate(dev); err != nil {
		t.Fatalf("Choose(stripe.Module) settled nothing: %v", err)
	}
	text, err := picked.Explain(dev)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "collect chose stripe.Module over paypal.Module for paymentcontracts.Provider") {
		t.Errorf("Explain does not say what was chosen:\n%s", text)
	}
}

func TestACycleIsNamedByTheModulesItPassesThrough(t *testing.T) {
	app := pkit.NewApp("collect").Use(cart.Module, collectibles.Module)
	says(t, app.Validate(dev), "pkit: collect: Use: cart → collectibles → cart is a cycle")
}

func TestTwoModulesCannotBothRunAfterEverything(t *testing.T) {
	app := pkit.NewApp("collect").Use(admin.Module, audit.Module)
	says(t, app.Validate(dev),
		"pkit: collect: Use: admin and audit both ask to run after everything: only one module may")
}

func TestOneLandingWithTwoContributorsAsksForChoose(t *testing.T) {
	app := pkit.NewApp("collect").Use(homepage.Module, shop.Module, blog.Module)
	says(t, app.Validate(dev),
		"pkit: collect: Choose: homepage takes one Landing, and shop and blog both contribute one: Choose in collect")

	picked := pkit.NewApp("collect").Use(homepage.Module, shop.Module, blog.Module).Choose(blog.Module)
	if err := picked.Validate(dev); err != nil {
		t.Fatalf("Choose(blog.Module) settled nothing: %v", err)
	}
}

func TestNothingMayNeedAModuleThatRunsAfterEverything(t *testing.T) {
	app := pkit.NewApp("collect").Use(admin.Module, reports.Module)
	says(t, app.Validate(dev),
		"pkit: collect: Use: reports needs admincontracts.Console, which admin provides after everything: nothing may need a module that runs after everything")
}

func TestChooseMayOnlyNameWhatUseComposed(t *testing.T) {
	app := pkit.NewApp("collect").Use(user.Module, wishlist.Module, stripe.Module, paypal.Module).
		Choose(shop.Module)
	says(t, app.Validate(dev),
		"pkit: collect: Choose: Choose(shop.Module) names a module that is not in Use: add it to Use or remove the choice")
}

func TestAChoiceThatSettlesNothingIsRefused(t *testing.T) {
	app := pkit.NewApp("collect").Use(user.Module, wishlist.Module, payment.Module).Choose(wishlist.Module)
	says(t, app.Validate(dev),
		"pkit: collect: Choose: Choose(wishlist.Module) chooses nothing: no contract it provides has a second provider")
}

func TestAModuleNamedTwiceIsRefused(t *testing.T) {
	app := pkit.NewApp("collect").Use(cart.Module, cart.Module)
	says(t, app.Validate(dev), "pkit: collect: Use: cart is in Use twice: remove one")
}

func TestTheDeploymentPicksTheImplementationHonestly(t *testing.T) {
	stripeInputs := map[string]string{"stripe.key": "sk_live_1", "stripe.webhook.secret": "whsec_1"}
	settled := pkit.NewApp("collect").Use(user.Module, wishlist.Module, payment.Module)
	if err := settled.Validate(production(stripeInputs)); err != nil {
		t.Fatalf("production with Stripe's inputs was refused: %v", err)
	}

	app := pkit.NewApp("collect").Use(payment.Module)
	says(t, app.Validate(production(nil)),
		"pkit: collect: Deployment: payment needs stripe.key in production: set it, or the deployment cannot pick payment")
	says(t, app.Validate(production(map[string]string{"stripe.webhook.secret": "whsec_1"})),
		"pkit: collect: Deployment: payment needs stripe.key in production: set it, or the deployment cannot pick payment")
}

func TestTheAfterEverythingModuleReadsTheWholeComposition(t *testing.T) {
	var saw []string
	adminProbe := pkit.NewModule("adminprobe", func(w *pkit.Wiring) (module.Module, error) {
		for _, m := range pkit.Composition(w) {
			saw = append(saw, m.Name)
		}
		return module.Module{Name: "adminprobe"}, nil
	}, pkit.After(pkit.Everything))

	if err := pkit.NewApp("collect").Use(user.Module, cart.Module, adminProbe).Validate(dev); err != nil {
		t.Fatalf("the after-everything module was refused: %v", err)
	}
	if strings.Join(saw, ",") != "user,cart" {
		t.Errorf("the after-everything module saw %v, want every other module's manifest", saw)
	}
}

func TestExplainReadsTheResolvedCompositionForItsEnvironment(t *testing.T) {
	app := pkit.NewApp("collect").Use(
		user.Module, wishlist.Module, cart.Module, shop.Module, homepage.Module,
		payment.Module, design.Module, admin.Module,
	)
	text, err := app.Explain(dev)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Explain of collect in development:\n%s", text)
	for _, line := range []string{
		"pkit: collect in development builds 8 modules.",
		"pkit: cart.Module is built after wishlist.Module.",
		"pkit: homepage.Module needs homepagecontracts.Landing from shop.Module.",
		"pkit: wishlist.Module uses paymentcontracts.Provider from payment.Module.",
		"pkit: wishlist.Module contributes one cartcontracts.Extension to cart.Module.",
		"pkit: payment.Module runs on manual, which the deployment picked.",
		"pkit: admin.Module runs after everything and saw every other module's manifest.",
		"pkit: design.Module describes [button field].",
	} {
		if !strings.Contains(text, line) {
			t.Errorf("Explain is missing %q:\n%s", line, text)
		}
	}

	if _, err := pkit.NewApp("collect").Use(collectibles.Module).Explain(dev); err == nil {
		t.Error("Explain answered a composition that does not resolve")
	}
}

// TestExplainCarriesWhatEachModuleDescribesToTheEnvironment pins the part of
// decision 0074 rule 4 the manifest already carries: the permissions a module
// defines and the events it emits belong in the composition file, next to the
// modules that provide and take.
func TestExplainCarriesWhatEachModuleDescribesToTheEnvironment(t *testing.T) {
	text, err := pkit.NewApp("collect").Use(user.Module, cart.Module, shipping.Module, rewards.Module).Explain(dev)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pkit: rewards.Module defines the permission rewards:read.",
		"pkit: shipping.Module emits shipping.labelled."} {
		if !strings.Contains(text, want) {
			t.Errorf("the composition file leaves out %q:\n%s", want, text)
		}
	}
}

// TestThreeFixtureModulesContributeToOneConsumerBuiltAfterThem is root's
// verdict's fixture row, composed of fixture modules: three of them contribute
// one contract's value, and the one consumer that takes every one of them is
// built after all three.
func TestThreeFixtureModulesContributeToOneConsumerBuiltAfterThem(t *testing.T) {
	got := &probe{}
	app := pkit.NewApp("collect").Use(user.Module, wishlist.Module, shipping.Module, rewards.Module,
		cart.Module, probeModule(got))
	if err := app.Validate(dev); err != nil {
		t.Fatalf("three contributors to one consumer were refused: %v", err)
	}
	if got.service == nil || got.service.Total() != 3 {
		t.Errorf("the consumer counted %v extensions, want wishlist's, shipping's and rewards'", got.service)
	}
	text, err := app.Explain(dev)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"pkit: cart.Module is built after wishlist.Module, shipping.Module and rewards.Module.",
		"pkit: cart.Module takes every cartcontracts.Extension from wishlist.Module, shipping.Module and rewards.Module.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Explain does not read %q:\n%s", want, text)
		}
	}
}
