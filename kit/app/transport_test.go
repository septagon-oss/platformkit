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
		opts.Transport = events.Memory()
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

func transportOptions(role Role) Options {
	return Options{Tenants: fixture{}, Authorize: fixture{}, Authenticate: anonymous, Role: role, Log: slog.New(slog.DiscardHandler)}
}
