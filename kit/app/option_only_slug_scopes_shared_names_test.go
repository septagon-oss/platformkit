package app

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// A composition that names its app in code must give that same name to the
// migration and transport. Both run after New has accepted the composition.
func TestAnOptionOnlyAppScopesItsMigrationAndBrokerTransport(t *testing.T) {
	cfg, opts := compose(t)
	opts.Role = Worker
	opts.App = appname.MustParse("acme")
	var brokerApp string
	opts.Transports.JetStream = func(settings config.NATS) (events.Transport, error) {
		brokerApp = settings.App
		return memory.New(), nil
	}

	a, err := New(t.Context(), cfg, nil, opts)
	if err != nil {
		t.Fatalf("compose app acme: %v", err)
	}
	if _, err := a.transport(); err != nil {
		t.Fatalf("construct the selected broker transport: %v", err)
	}
	if brokerApp != "acme" {
		t.Errorf("broker constructor received app %q, want acme", brokerApp)
	}
	declaration, err := migrationDeclaration(a.cfg)
	if err != nil {
		t.Fatalf("form the migration declaration: %v", err)
	}
	declaredApp := "<nil>"
	if declaration.App != nil {
		declaredApp = *declaration.App
	}
	if declaredApp != "acme" {
		t.Errorf("migration declaration names app %q, want acme", declaredApp)
	}
}
