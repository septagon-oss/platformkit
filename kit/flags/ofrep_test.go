package flags

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"maps"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOFREPTLSWireAndCleanup(t *testing.T) {
	var mode, calls, connections, redirected atomic.Int32
	closed := make(chan struct{}, 8)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		_, _ = io.WriteString(w, `{"value":true}`)
	}))
	defer target.Close()
	subject := Subject{TenantID: uuid.New(), TargetingKey: "actor-one"}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct{ Context map[string]any }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		key, _ := json.Marshal([]string{"tasks", "customer-one", "test", subject.TenantID.String(), "actor-one"})
		want := map[string]any{"application": "tasks", "installation": "customer-one", "environment": "test",
			"tenant_id": subject.TenantID.String(), "subject": "actor-one", "targetingKey": string(key)}
		if r.Method != http.MethodPost || r.URL.Path != "/prefix/ofrep/v1/evaluate/flags/editor.v2-beta_1" ||
			r.Header.Get("Authorization") != "Bearer test-token" || !maps.Equal(request.Context, want) || r.TLS == nil {
			t.Error("TLS request lost its endpoint, authorization or trusted scope")
		}
		switch mode.Load() {
		case 2:
			_, _ = io.WriteString(w, `{"value":"wrong","reason":"STATIC"}`)
		case 3:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 4:
			w.WriteHeader(http.StatusNotFound)
		case 5:
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
		case 6:
			<-r.Context().Done()
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"value": mode.Load() == 0, "variant": "rollout", "reason": "STATIC"})
		}
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		} else if state == http.StateClosed {
			closed <- struct{}{}
		}
	}
	server.StartTLS()
	defer server.Close()
	config := OFREPConfig{Scope: Scope{"tasks", "customer-one", "test"}, URL: server.URL + "/prefix",
		CertificatePath: writeOFREPTestCA(t, server.Certificate().Raw), BearerToken: "test-token", Timeout: time.Second}
	evaluator, err := NewOFREP(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer evaluator.Close(context.Background())
	if calls.Load() != 0 || connections.Load() != 0 {
		t.Fatal("construction contacted the endpoint")
	}
	for _, tc := range []struct {
		mode     int32
		fallback bool
		want     bool
		err      error
	}{
		{0, false, true, nil}, {1, true, false, nil},
		{2, false, false, ErrTypeMismatch}, {3, true, true, ErrUnavailable},
		{4, true, true, ErrNotFound}, {5, false, false, ErrUnavailable},
	} {
		mode.Store(tc.mode)
		decision, err := evaluator.Boolean(t.Context(), "editor.v2-beta_1", subject, tc.fallback)
		if !errors.Is(err, tc.err) || decision.Value != tc.want || decision.Defaulted != (tc.err != nil) {
			t.Fatalf("mode %d: decision=%+v error=%v", tc.mode, decision, err)
		}
		if tc.err == nil && decision.Variant != "rollout" {
			t.Fatalf("wire variant lost: %+v", decision)
		}
	}
	if calls.Load() != 6 || redirected.Load() != 0 {
		t.Fatal("a response was cached or a redirect was followed")
	}
	for _, key := range []string{".", "..", "../other", "a/b", "a%2fb", "a?query", "a#fragment", `a\b`, "é"} {
		decision, err := evaluator.Boolean(t.Context(), key, subject, true)
		if !errors.Is(err, ErrInvalid) || !decision.Value || !decision.Defaulted {
			t.Fatalf("unsafe path key accepted: %+v %v", decision, err)
		}
	}
	if calls.Load() != 6 {
		t.Fatal("an unsafe key reached the service")
	}
	if err := evaluator.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close retained an idle TLS connection")
	}
	// A second composition owns its connection and honors caller cancellation.
	evaluator, err = NewOFREP(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer evaluator.Close(context.Background())
	mode.Store(6)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	decision, err := evaluator.Boolean(ctx, "editor.v2-beta_1", subject, true)
	if !errors.Is(err, context.DeadlineExceeded) || !decision.Value || !decision.Defaulted {
		t.Fatalf("wire cancellation lost: %+v %v", decision, err)
	}
}

func TestOFREPConfigurationAndLazyTrust(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"value":true}`)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	base := OFREPConfig{Scope: Scope{"app", "one", "test"}, URL: server.URL, Timeout: time.Second}
	malformed := writeOFREPTestCA(t, []byte("not a certificate"))
	for _, change := range []func(*OFREPConfig){
		func(c *OFREPConfig) { c.URL = "http://service.internal"; c.Insecure = true },
		func(c *OFREPConfig) { c.URL = "http://127.0.0.1" },
		func(c *OFREPConfig) { c.URL = "https://user:password@service.internal" },
		func(c *OFREPConfig) { c.URL += "?query" },
		func(c *OFREPConfig) { c.URL += "?" },
		func(c *OFREPConfig) { c.URL += "#fragment" },
		func(c *OFREPConfig) { c.URL += "#" },
		func(c *OFREPConfig) { c.URL = "/relative" },
		func(c *OFREPConfig) { c.Scope.Installation = "" },
		func(c *OFREPConfig) { c.Timeout = 0 },
		func(c *OFREPConfig) { c.BearerToken = "bad\nheader" },
		func(c *OFREPConfig) { c.CertificatePath = t.TempDir() + "/missing.pem" },
		func(c *OFREPConfig) { c.CertificatePath = malformed },
	} {
		config := base
		change(&config)
		if evaluator, err := NewOFREP(t.Context(), config); evaluator != nil || !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid configuration accepted: %v %v", evaluator, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if evaluator, err := NewOFREP(ctx, base); evaluator != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled construction accepted: %v %v", evaluator, err)
	}
	base.CertificatePath = writeOFREPTestCA(t, unrelatedOFREPTestCA(t))
	evaluator, err := NewOFREP(t.Context(), base)
	if err != nil {
		t.Fatalf("lazy construction attempted a handshake: %v", err)
	}
	defer evaluator.Close(context.Background())
	decision, err := evaluator.Boolean(t.Context(), "editor", Subject{uuid.New(), "actor"}, true)
	if !errors.Is(err, ErrUnavailable) || !decision.Value || !decision.Defaulted || calls.Load() != 0 {
		t.Fatalf("untrusted TLS server accepted: %+v %v calls=%d", decision, err, calls.Load())
	}
}

func TestOFREPExplicitLoopbackIgnoresAmbientProviderConfiguration(t *testing.T) {
	for name, value := range map[string]string{"FLAGD_HOST": "other-service", "FLAGD_RESOLVER": "file", "FLAGD_TLS": "true",
		"HTTP_PROXY": "http://127.0.0.1:1", "HTTPS_PROXY": "http://127.0.0.1:1"} {
		t.Setenv(name, value)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"value":true,"reason":"STATIC"}`)
	}))
	defer server.Close()
	evaluator, err := NewOFREP(t.Context(), OFREPConfig{Scope: Scope{"app", "one", "test"}, URL: server.URL, Insecure: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer evaluator.Close(context.Background())
	if decision, err := evaluator.Boolean(t.Context(), "editor", Subject{uuid.New(), "actor"}, false); err != nil || !decision.Value {
		t.Fatalf("explicit loopback configuration was overridden: %+v %v", decision, err)
	}
}

func writeOFREPTestCA(t *testing.T, certificate []byte) string {
	t.Helper()
	path := t.TempDir() + "/ca.pem"
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func unrelatedOFREPTestCA(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}
