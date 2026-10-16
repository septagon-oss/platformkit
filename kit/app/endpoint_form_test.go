package app

// Two documents in this repository name the shape of
// telemetry.otlp_endpoint, and the code refuses it.
//
//   config.example.yaml:  The collector's gRPC endpoint: "collector.example:4317",
//                         or a host:port on a network that needs no TLS.
//   kit/config/config.go: OTLPEndpoint is the collector's URL —
//                         https://collector.example:4318, or a host:port for a
//                         collector on the same network with no TLS.
//
// collector(), the line that decides whether the boot continues, accepts only a
// value whose scheme is http or https, so the form both documents print stops the
// application from starting at all — which e9fc2f9's own message calls the one
// thing this key must not do silently ("a typo in a key that decides where spans
// go should stop the boot") while naming host:port as the ordinary local answer.
//
// The case asks for what the documents promise. The other possible fix — printing
// the refused form in both files instead — is a change to this case, not a pass for
// it, and the review says so; what must not survive is a repository whose example
// configuration cannot be run.

import (
	"log/slog"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
)

func TestTheEndpointFormTheConfigurationExamplePrintsIsAccepted(t *testing.T) {
	for _, endpoint := range []string{"collector.example:4317", "localhost:4317"} {
		shutdown, report, err := installTelemetry(t.Context(), config.Telemetry{
			OTLPEndpoint: endpoint,
			ServiceName:  "pkit-endpoint-form",
		}, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Errorf("installTelemetry(%q) = %v; config.example.yaml and kit/config both print a "+
				"bare host:port as an accepted telemetry.otlp_endpoint, and a value either form may take "+
				"must not be the one that stops the boot", endpoint, err)
			continue
		}
		if report == nil {
			t.Errorf("installTelemetry(%q) reported no exporter health beside an exporter that exists", endpoint)
		}
		if err := shutdown(t.Context()); err != nil {
			t.Errorf("shutdown of the %q providers: %v", endpoint, err)
		}
	}
}
