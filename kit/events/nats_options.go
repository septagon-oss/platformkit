package events

import (
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/septagon-oss/platformkit/kit/config"
)

// ConnectJetStream constructs the existing transport from PlatformKit settings.
// TLS verifies the endpoint hostname; CACert supplies private trust roots and an
// empty value retains system trust. The returned Transport is an io.Closer.
func ConnectJetStream(settings config.NATS) (Transport, error) {
	if err := settings.Validate(); err != nil {
		return nil, fmt.Errorf("events: %w", err)
	}
	var options []nats.Option
	if settings.Username != "" {
		options = append(options, nats.UserInfo(settings.Username, settings.Password))
	}
	if settings.CACert != "" {
		options = append(options, nats.RootCAs(settings.CACert))
	}
	return JetStream(settings.URL, options...)
}
