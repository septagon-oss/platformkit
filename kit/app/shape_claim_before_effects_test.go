package app

// The event shapes a composition declares are process state, and one process holds
// one shape per name. New reads the catalog and answers the clash already standing,
// but two boots can both read it before either writes it, so the claim itself is a
// claim — refused, installing nothing — and it sits above the connection rather than
// after the transport, which is the only place a refusal about it can cost the
// deployment nothing. These are the two claims that placement is what makes true.

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
	// New asks the catalog what is standing, and nothing names this event yet.
	a, err := New(t.Context(), cfg, []module.Module{hello(), declaringLedger()}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// The other boot of the race: the same name under another payload, installed
	// after this boot read the catalog and before it claims.
	live, err := events.DeclareMore([]events.Declared{events.Declare[claimedNumber]("ledger.posted")})
	if err != nil {
		t.Fatalf("install the other composition's shape: %v", err)
	}
	t.Cleanup(live)

	rt, err := a.Start(t.Context())
	if rt != nil {
		_ = rt.Close()
		t.Fatal("Start returned a Runtime whose event name another composition holds")
	}
	if err == nil || !strings.Contains(err.Error(), "ledger.posted") {
		t.Fatalf("Start did not refuse the event name this process already answers: %v", err)
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
