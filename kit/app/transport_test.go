package app

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	eventnats "github.com/septagon-oss/platformkit/kit/events/providers/nats"
)

func TestNATSTransportSelection(t *testing.T) {
	endpoint := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if endpoint == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset; start the test stack with make up")
	}
	for _, test := range []struct {
		role   Role
		mode   string
		broker bool
	}{
		{All, "", false}, {All, "memory", false}, {All, "jetstream", true},
		{Worker, "", true}, {Worker, "jetstream", true},
	} {
		t.Run(string(test.role)+"/"+test.mode, func(t *testing.T) {
			cfg := config.Config{NATS: config.NATS{URL: endpoint, Transport: test.mode}}
			a, err := New(t.Context(), cfg, nil, transportOptions(test.role))
			if err != nil {
				t.Fatal(err)
			}
			transport, err := a.transport()
			if err != nil {
				t.Fatal(err)
			}
			closer, broker := transport.(io.Closer)
			if broker {
				defer closer.Close()
				event := events.Event{ID: uuid.New(), TenantID: uuid.New(), Name: "test_transport_" + strings.ReplaceAll(uuid.NewString(), "-", "") + ".selected"}
				if err := transport.Publish(t.Context(), event); err != nil {
					t.Fatalf("selected broker did not acknowledge the event: %v", err)
				}
			}
			if broker != test.broker {
				t.Fatal("role and explicit transport did not select the expected provider")
			}
		})
	}
}

func TestInvalidNATSTransportFailsBeforeOpeningApplicationResources(t *testing.T) {
	for _, test := range []struct {
		role Role
		mode string
		url  string
	}{
		{Worker, "memory", "nats://localhost:4222"},
		{Web, "memory", "nats://localhost:4222"},
		{All, "unknown", "nats://localhost:4222"},
		{All, "jetstream", "invalid"},
		{Worker, "", "invalid"},
	} {
		t.Run(string(test.role)+"/"+test.mode, func(t *testing.T) {
			cfg := config.Config{NATS: config.NATS{URL: test.url, Transport: test.mode}}
			if _, err := New(t.Context(), cfg, nil, transportOptions(test.role)); err == nil {
				t.Fatal("invalid transport composition passed the boot gate")
			}
		})
	}
}

func TestInjectedTransportOverridesNATSSettings(t *testing.T) {
	for _, role := range []Role{All, Web, Worker} {
		opts := transportOptions(role)
		opts.Transport = memory.New()
		cfg := config.Config{NATS: config.NATS{URL: "invalid", Transport: "unknown"}}
		a, err := New(t.Context(), cfg, nil, opts)
		if err != nil {
			t.Fatal(err)
		}
		got, err := a.transport()
		if err != nil || got != opts.Transport {
			t.Fatal("explicit provider was not retained")
		}
	}
}

// TestNewRefusesAMissingTransportConstructor: the kernel knows the two names
// and builds neither, so a composition that names one and supplies no
// constructor for it is refused before anything is opened — in every role,
// because the web half of the same image must answer like the worker half.
func TestNewRefusesAMissingTransportConstructor(t *testing.T) {
	for _, test := range []struct {
		role       Role
		mode       string
		transports Transports
		missing    string
	}{
		{All, "", Transports{JetStream: eventnats.Connect}, "Transports.Memory"},
		{All, "memory", Transports{}, "Transports.Memory"},
		{All, "jetstream", Transports{Memory: memory.New}, "Transports.JetStream"},
		{Worker, "", Transports{Memory: memory.New}, "Transports.JetStream"},
		{Web, "", Transports{Memory: memory.New}, "Transports.JetStream"},
	} {
		t.Run(string(test.role)+"/"+test.mode, func(t *testing.T) {
			opts := transportOptions(test.role)
			opts.Transports = test.transports
			cfg := config.Config{NATS: config.NATS{URL: "nats://localhost:4222", Transport: test.mode}}
			_, err := New(t.Context(), cfg, nil, opts)
			if err == nil || !strings.Contains(err.Error(), test.missing) {
				t.Fatalf("New = %v, want the missing %s named", err, test.missing)
			}
		})
	}
}

// transportOptions is a composition that supplies both constructors, the way
// apps/platformkit does; what varies per test is the role and nats.transport.
func transportOptions(role Role) Options {
	return Options{Tenants: fixture{}, Authorize: fixture{}, Authenticate: anonymous, Role: role, Log: slog.New(slog.DiscardHandler),
		Transports: Transports{Memory: memory.New, JetStream: eventnats.Connect}}
}
