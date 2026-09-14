package nats_test

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/config"
	provider "github.com/septagon-oss/platformkit/kit/events/providers/nats"
)

func TestOwnedNATSProviderUsesPrivateTLSAndCredentials(t *testing.T) {
	certificateServer := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := certificateServer.TLS.Certificates[0]
	certificateServer.Close()
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name                   string
		trusted, wrongHostname bool
	}{
		{"private CA", true, false},
		{"untrusted CA", false, false},
		{"wrong hostname", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			type observation struct {
				line string
				err  error
			}
			observed := make(chan observation, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					observed <- observation{err: err}
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				_, err = conn.Write([]byte("INFO {\"server_id\":\"fixture\",\"tls_required\":true,\"auth_required\":true}\r\n"))
				secured := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
				if err == nil {
					err = secured.Handshake()
				}
				if err != nil {
					observed <- observation{err: err}
					return
				}
				line, err := bufio.NewReader(secured).ReadString('\n')
				observed <- observation{line: line, err: err}
				// Refuse after CONNECT so the real client reports a safe failure;
				// this fixture does not emulate the JetStream stream protocol.
				_, _ = secured.Write([]byte("-ERR 'Authorization Violation'\r\n"))
			}()
			settings := config.NATS{URL: "tls://" + listener.Addr().String(), Username: "fixture-user", Password: "credential-canary"}
			if test.trusted {
				settings.CACert = caPath
			}
			if test.wrongHostname {
				_, port, _ := net.SplitHostPort(listener.Addr().String())
				settings.URL = "tls://localhost:" + port
			}
			transport, err := provider.Connect(settings)
			if err == nil || transport != nil || strings.Contains(err.Error(), settings.Password) {
				t.Fatal("refused authentication must return a credential-free error")
			}
			result := <-observed
			if !test.trusted || test.wrongHostname {
				if result.err == nil || result.line != "" {
					t.Fatal("credentials reached a server without a trusted TLS certificate")
				}
				return
			}
			var connect struct{ User, Pass string }
			if result.err != nil || !strings.HasPrefix(result.line, "CONNECT ") {
				t.Fatalf("trusted TLS connection did not reach CONNECT: %v", result.err)
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(result.line, "CONNECT ")), &connect); err != nil {
				t.Fatal(err)
			}
			if connect.User != settings.Username || connect.Pass != settings.Password {
				t.Fatal("PlatformKit credentials were not used by the provider")
			}
		})
	}
}

func TestOwnedNATSProviderRefusesMissingAndInvalidTrustRoots(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "credential-canary.pem")
	for _, exists := range []bool{false, true} {
		if exists {
			if err := os.WriteFile(missing, []byte("credential-canary"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		transport, err := provider.Connect(config.NATS{URL: "tls://broker.invalid:4222", CACert: missing})
		if err == nil || transport != nil || strings.Contains(err.Error(), "credential-canary") {
			t.Fatal("unreadable trust roots must fail without exposing input")
		}
		if !exists && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal("missing trust-root error lost its underlying cause")
		}
	}
}
