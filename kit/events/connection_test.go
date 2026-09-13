package events_test

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/septagon-oss/platformkit/kit/events"
)

type refusedNATSDial struct{ cause error }

func (d refusedNATSDial) Dial(string, string) (net.Conn, error) { return nil, d.cause }

func TestJetStreamConnectionErrorsDoNotExposeCredentials(t *testing.T) {
	for _, test := range []struct{ name, endpoint string }{
		{"password", "nats://fixture-user:fixture-credential-canary@broker.invalid:4222"},
		{"token", "nats://fixture-credential-canary@broker.invalid:4222"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New("fixture refused with fixture-credential-canary")
			transport, err := events.JetStream(test.endpoint, nats.SetCustomDialer(refusedNATSDial{cause}))
			if transport != nil || err == nil {
				t.Fatal("a refused connection must return only an error")
			}
			if strings.Contains(err.Error(), "fixture-credential-canary") || strings.Contains(err.Error(), "fixture-user") {
				t.Fatal("connection diagnostics exposed credentials")
			}
			if !errors.Is(err, cause) {
				t.Fatal("connection diagnostics lost the underlying refusal")
			}
		})
	}
}

func TestJetStreamMalformedEndpointKeepsItsCausePrivate(t *testing.T) {
	_, err := events.JetStream("nats://fixture-user:fixture-credential-canary@[invalid")
	if err == nil {
		t.Fatal("malformed endpoint was accepted")
	}
	if strings.Contains(err.Error(), "fixture-credential-canary") || strings.Contains(err.Error(), "fixture-user") {
		t.Fatal("connection diagnostics exposed malformed endpoint credentials")
	}
	cause, ok := errors.AsType[*url.Error](err)
	if !ok || !errors.Is(err, cause.Err) {
		t.Fatal("URL parsing cause is no longer discoverable")
	}
}

func TestJetStreamAcceptsOfficialTLSAndAuthenticationOptions(t *testing.T) {
	roots := x509.NewCertPool()
	cause := errors.New("fixture option failure: fixture-credential-canary")
	configured := false
	_, err := events.JetStream("tls://broker.invalid:4222",
		nats.Secure(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}),
		nats.UserInfo("fixture-user", "fixture-credential-canary"),
		func(options *nats.Options) error {
			configured = true
			if options.Name != "platformkit" || options.MaxReconnect != -1 {
				t.Error("transport defaults were not supplied before application options")
			}
			if !options.Secure || options.TLSConfig.RootCAs != roots || options.TLSConfig.InsecureSkipVerify {
				t.Error("verified TLS configuration was not retained")
			}
			if options.User != "fixture-user" || options.Password != "fixture-credential-canary" {
				t.Error("typed authentication options were not retained")
			}
			return cause
		},
	)
	if !configured || !errors.Is(err, cause) {
		t.Fatal("application options were not applied with a discoverable failure")
	}
	if strings.Contains(err.Error(), "fixture-credential-canary") {
		t.Fatal("option error exposed credentials")
	}
}
