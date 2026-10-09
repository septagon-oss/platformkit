package app

// The event shapes a composition declares are catalog state, and one app holds one
// shape per name. New reads the catalog, answers the clash already standing, and
// takes the claim on its last line under the catalog's own lock: the composition
// that spells one of its app's event names another way is refused with its
// deployment undialled, and two boots that would both read the catalog before either
// wrote it are answered by the one that reaches the claim second. What that
// placement is what makes true is here: a refusal about a shape has spent no pool,
// no migration and no store, and it has changed nothing the composition standing is
// answering under.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
)

type claimedNumber struct {
	Number int64 `json:"number"`
}

// claimedText names one event with a payload that is not the same document.
type claimedText struct {
	Number string `json:"number"`
}

// declaringLedger is a module whose one event is spelled with a text payload.
func declaringLedger() module.Module {
	return module.Module{Name: "ledger", Declared: []events.Declared{
		events.Declare[claimedText]("ledger.posted"),
	}}
}

// undialableDatabase points the deployment at a port nothing answers on: a boot that
// stopped before the connection says why in its own words, and one that did not gets
// a connection sentence instead. The store constructor counts the calls beside it.
func undialableDatabase(cfg *config.Config) {
	never := "postgres://platformkit:platformkit@127.0.0.1:1/never?sslmode=disable"
	cfg.Database = config.Database{URL: never, MigrateURL: never}
}

func TestARefusedEventShapeIsAnsweredWithTheDeploymentUndialed(t *testing.T) {
	cfg, opts := compose(t)
	undialableDatabase(&cfg)
	built := 0
	opts.Caches = sharedStore(&cfg, &built, nil)
	// The other composition of this app, first to the claim: the same name under
	// another payload, standing before this boot is even composed.
	live, err := events.DeclareMore([]events.Declared{events.Declare[claimedNumber]("ledger.posted")})
	if err != nil {
		t.Fatalf("install the other composition's shape: %v", err)
	}
	t.Cleanup(live)

	a, err := New(t.Context(), cfg, []module.Module{hello(), declaringLedger()}, opts)
	if a != nil {
		t.Fatal("New handed back an application whose event name another composition holds")
	}
	if err == nil || !strings.Contains(err.Error(), "ledger.posted") {
		t.Fatalf("New did not refuse the event name this app already answers: %v", err)
	}
	if built != 0 {
		t.Errorf("the refused boot built the store the deployment named %d times on the way to the shape refusal", built)
	}
	// The mark is what the caller behind this package reads: a lifecycle written on
	// the way to a boot answered before the connection is returnable.
	if !RefusedBeforeEffects(err) {
		t.Errorf("the shape refusal carries no pre-effect mark, so its caller would keep a lifecycle the deployment never spent: %v", err)
	}
	// The refusal installed nothing: the shape the live grip holds still describes
	// this process, and the refused spelling is free again once that grip is given
	// back — a claim refused is a claim that was never taken.
	if err := events.CheckDeclared([]events.Declared{events.Declare[claimedNumber]("ledger.posted")}); err != nil {
		t.Errorf("the standing shape stopped describing this process beside a refused boot: %v", err)
	}
	live()
	if err := events.CheckDeclared([]events.Declared{events.Declare[claimedText]("ledger.posted")}); err != nil {
		t.Errorf("the refused spelling stayed claimed after its boot came back: %v", err)
	}
}

// The claim is taken by the composition, not by the boot that serves: a New that
// succeeded holds its app's names from that moment, so a second composition of the
// same app that means something else by one of them is refused beside it, and the
// release of what that composition added is what frees the name again.
func TestAComposedApplicationHoldsItsEventNamesUntilItsRelease(t *testing.T) {
	cfg, opts := compose(t)
	undialableDatabase(&cfg)
	built := 0
	opts.Caches = sharedStore(&cfg, &built, nil)

	first, err := New(t.Context(), cfg, []module.Module{hello(), declaringLedger()}, opts)
	if err != nil {
		t.Fatalf("compose the first spelling: %v", err)
	}
	second, err := New(t.Context(), cfg, []module.Module{hello(), declaringLedger()}, opts)
	if err != nil {
		t.Fatalf("a second composition of one shape was refused: %v", err)
	}
	if second == nil {
		t.Fatal("New returned no application and no refusal")
	}
	if _, err := New(t.Context(), cfg, []module.Module{hello(), declaringLedgerNumber()}, opts); err == nil {
		t.Fatal("a composition claimed an event name this app already answers another way")
	} else if !strings.Contains(err.Error(), "ledger.posted") || !RefusedBeforeEffects(err) {
		t.Errorf("the disagreeing composition was refused for the wrong reason or after an effect: %v", err)
	}
	// Both compositions gave back what they added, and the name is free.
	first.giveBackDeclared()
	second.giveBackDeclared()
	if err := events.CheckDeclared([]events.Declared{events.Declare[claimedNumber]("ledger.posted")}); err != nil {
		t.Errorf("the claim of two compositions that were never started stayed on the name after both were given back: %v", err)
	}
}

// declaringLedgerNumber is the same manifest with the payload that is not the same
// document: the disagreement the claim refuses.
func declaringLedgerNumber() module.Module {
	return module.Module{Name: "ledger", Declared: []events.Declared{
		events.Declare[claimedNumber]("ledger.posted"),
	}}
}
