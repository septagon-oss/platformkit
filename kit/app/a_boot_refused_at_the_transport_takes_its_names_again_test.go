package app

// The event names a composition declares are claimed by its app for as long as one of its
// boots is standing (kit/events NewCatalog), and a boot that ends gives the claim back: a
// Runtime that Closes, and a boot that Start refused after it had opened the deployment —
// the transport is the last thing Start opens and the last thing that can refuse it. Both
// answers empty the grip, so the boot that comes after either one has to take the claim
// again, which is what claimDeclared is for. A retry that took nothing would serve
// publishes the process-wide record no longer measures, and let a second composition of
// the same app answer the declared name under another payload beside it. This case asks the
// question at the transport refusal, whose release runs inside Start.

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
)

// bootAmount is what this composition's module answers its event with; bootText is the
// same name spelled as a document that is not it, which only a composition holding no
// claim would let stand beside the first.
type bootAmount struct {
	Total int64 `json:"total"`
}

type bootText struct {
	Total string `json:"total"`
}

const (
	bootApp   = "bootretry"
	bootEvent = "bootretry.ledger_posted"
)

// declaringLedgerBoot is a module that declares the one event and mounts the one route a
// composition has to offer on the workspace surface for its boot to be buildable at all.
func declaringLedgerBoot() module.Module {
	return module.Module{
		Name:     "bootretry",
		Declared: []events.Declared{events.Declare[bootAmount](bootEvent)},
		Routes: func(s httpx.Surfaces) {
			httpx.Register(s.App, huma.Operation{OperationID: "bootRetry", Method: http.MethodGet, Path: "/retry"},
				httpx.Public(), func(context.Context, *struct{}) (*helloOut, error) {
					return &helloOut{}, nil
				})
		},
	}
}

// TestABootRefusedAtTheTransportTakesItsEventNamesAgain walks one App through a boot the
// transport refused and the boot that follows it, asking at each step whether a disagreeing
// shape is refused beside the composition: held by New, given back by the refused boot,
// held again by the boot that retries.
func TestABootRefusedAtTheTransportTakesItsEventNamesAgain(t *testing.T) {
	cfg, opts := compose(t)
	opts.App = appname.MustParse(bootApp)
	cfg.NATS.Transport = "jetstream"
	// The broker constructor answers the first boot with a refusal and the second with
	// the in-process transport, so the one App is booted twice over the same names.
	calls := 0
	opts.Transports = Transports{Memory: memory.New, JetStream: func(config.NATS) (events.Transport, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("the broker refused this boot")
		}
		return memory.New(), nil
	}}

	disagreeing := []events.Declared{events.Declare[bootText](bootEvent)}
	own := []events.Declared{events.Declare[bootAmount](bootEvent)}

	a, err := New(t.Context(), cfg, []module.Module{declaringLedgerBoot()}, opts)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	// Whatever this case concludes, its own app's names go back before the next case
	// in the package reads the process-wide record.
	t.Cleanup(a.giveBackDeclared)
	if err := events.CheckAppDeclared(opts.App, disagreeing); err == nil {
		t.Fatal("a composition that New built holds no claim on the names it declares")
	}

	rt, err := a.Start(t.Context())
	if err == nil {
		_ = rt.Close()
		t.Fatal("a boot whose transport constructor refused was accepted")
	}
	// The refusal gave the names back: that is what lets the corrected boot mean what
	// it says, and it is why the retry has to claim them again.
	if err := events.CheckAppDeclared(opts.App, disagreeing); err != nil {
		t.Fatalf("the boot the transport refused kept its names: %v", err)
	}

	rt2, err := a.Start(t.Context())
	if err != nil {
		t.Fatalf("start the composition again: %v", err)
	}
	defer func() { _ = rt2.Close() }()

	if err := events.CheckAppDeclared(opts.App, disagreeing); err == nil {
		t.Error("the boot after a transport refusal runs with no claim on the event names it declares: " +
			"a second composition of the same app may answer one name under another payload beside it")
	}
	// The shape the module itself declares is still accepted beside the retry, so the
	// answer above is about the disagreeing payload and not about a second call.
	if err := events.CheckAppDeclared(opts.App, own); err != nil {
		t.Errorf("the retry refuses the shape its own module declared: %v", err)
	}
}
