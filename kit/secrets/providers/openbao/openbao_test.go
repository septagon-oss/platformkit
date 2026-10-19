package openbao_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/secrets"
	"github.com/septagon-oss/platformkit/kit/secrets/internal/box"
	"github.com/septagon-oss/platformkit/kit/secrets/providers/openbao"
	"github.com/septagon-oss/platformkit/kit/secrets/secretstest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The stub answers the two endpoints this adapter speaks, with the two promises
// they make: KV v2's version moves with the bytes and a soft delete is a miss, and
// transit derives a different key per context, so a ciphertext written for one
// tenant will not open with another's. It is a fake of the *wire*, which is what
// this package reads and what a status map can get wrong; the container run below is
// the evidence about OpenBao itself, and it is the same case list.
type stub struct {
	mu      sync.Mutex
	secrets map[string]stubSecret
	keys    map[string][]byte
	status  int
}

type stubSecret struct {
	value   string
	version int
	deleted string
}

func newStub() *stub {
	return &stub{secrets: map[string]stubSecret{}, keys: map[string][]byte{}}
}

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != 0 {
		w.WriteHeader(s.status)
		_, _ = fmt.Fprint(w, `{"errors":["status refused"]}`)
		return
	}
	if r.Header.Get("X-Vault-Token") == "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/")
	switch {
	case strings.HasPrefix(path, "secret/data/"):
		entry, ok := s.secrets[strings.TrimPrefix(path, "secret/data/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"errors":["not found"]}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"data":     map[string]string{"value": entry.value},
			"metadata": map[string]any{"version": entry.version, "deletion_time": entry.deleted},
		}})
	case strings.HasPrefix(path, "transit/encrypt/") || strings.HasPrefix(path, "transit/decrypt/"):
		s.transit(w, r, strings.SplitN(path, "/", 3))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// transit seals and opens the way a derived transit key does: the context is folded
// into the key's additional authenticated data, so the same key answers a different
// key for every tenant and a ciphertext cannot cross.
//
// The one refusal a fake of this endpoint has to copy is the server's own: OpenBao
// answers `transit/encrypt/<tenant>/<key>` with 404 "unsupported path", because a
// transit key lives at one path segment and cannot be nested the way a KV secret
// can. A stub that accepted the nesting is how an adapter passed a suite and then
// met a server that had never heard of the address.
func (s *stub) transit(w http.ResponseWriter, r *http.Request, parts []string) {
	// The key is mounted under its own address, which for a customer's key is its
	// tenant's UUID as a prefix — see openbao.KeyAddress — and not a directory.
	if len(parts) < 3 || strings.Contains(parts[2], "/") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	material, ok := s.keys[parts[2]]
	if !ok {
		// A key that was never mounted is the server's 400 "encryption key not
		// found", measured against the container, and not a miss.
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	key, err := box.Key(material)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	aad, _ := base64.StdEncoding.DecodeString(body["context"])
	envelope := func(in string) ([]byte, error) {
		if parts[1] == "encrypt" {
			raw, err := base64.StdEncoding.DecodeString(in)
			if err != nil {
				return nil, err
			}
			return key.Seal(aad, raw)
		}
		// The server's own envelope comes back off the wire as `vault:v<N>:` plus the
		// payload, and the version it names is the key version that wrote it — which
		// this stub does not rotate, so any version it emitted is its own to open.
		_, payload, found := strings.Cut(in, ":")
		if !found {
			return nil, errors.New("no envelope")
		}
		_, payload, _ = strings.Cut(payload, ":")
		raw, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, err
		}
		return key.Open(aad, raw)
	}
	out, err := envelope(body["plaintext"] + body["ciphertext"])
	if err != nil {
		// A ciphertext this key (in this context) did not write is the server's 400,
		// which is what the adapter turns into ErrSealed.
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"errors":["bad request"]}`)
		return
	}
	if parts[1] == "encrypt" {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"ciphertext": "vault:v1:" + base64.StdEncoding.EncodeToString(out), "key_version": 1,
		}})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
		"plaintext": base64.StdEncoding.EncodeToString(out),
	}})
}

var tenantA = uuid.MustParse("11111111-1111-4111-8111-111111111111")
var tenantB = uuid.MustParse("22222222-2222-4222-8222-222222222222")

// TestTheOpenBaoAdapterConforms runs the shared suite against the wire shape.
func TestTheOpenBaoAdapterConforms(t *testing.T) {
	server := newStub()
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	cfg := config.Secrets{Adapter: "openbao", URL: httpServer.URL, Token: "a-token-never-quoted"}

	adapter := secretstest.Adapter{
		Name:  "openbao",
		Store: func(*testing.T) secrets.Store { return mustStore(t, cfg) },
		Failing: func(*testing.T) secrets.Store {
			return mustStore(t, config.Secrets{Adapter: "openbao", URL: "http://127.0.0.1:1", Token: "x"})
		},
		Write: func(t *testing.T, ref secrets.Ref, plaintext []byte) {
			t.Helper()
			server.mu.Lock()
			defer server.mu.Unlock()
			name := pathOf(ref)
			entry := server.secrets[name]
			server.secrets[name] = stubSecret{value: string(plaintext), version: entry.version + 1}
		},
		WriteKey: func(t *testing.T, ref secrets.Ref) {
			t.Helper()
			server.mu.Lock()
			defer server.mu.Unlock()
			// Two names are two keys: the binding a key service offers is the key
			// itself plus the derivation context, not the spelling in a path.
			material := secretstest.KeyMaterial
			if ref.Name() == secretstest.SuiteKeyOther {
				material = secretstest.OtherKeyMaterial
			}
			server.keys[openbao.KeyAddress(ref)] = []byte(material)
		},
		// A KV v2 `value` is a JSON string, so the bytes this store can hold are the
		// ones JSON can spell — NUL, newline and a non-ASCII letter included. The
		// suite's nastiest byte is refused by the store rather than corrupted by the
		// adapter, and the README's Limits names the difference. Declaring it here is
		// the point: an adapter that quietly lost the byte would pass the case.
		Exact: []byte("hunter2\n\u00e9\u4e2d\u0000"),
	}
	secretstest.Conformance(t, adapter)
	secretstest.Tenants(t, adapter)
	secretstest.Versions(t, adapter)
	secretstest.Keys(t, adapter)
	secretstest.TenantKeys(t, adapter)
}

// TestTheStatusMap is the one thing an adapter of an HTTP service cannot delegate:
// which answer means "not here", which means "you may not", and which means "it is
// down". A caller that fails open on a miss fails closed on the last two, and can
// only do that if this table is right.
func TestTheStatusMap(t *testing.T) {
	for name, status := range map[string]int{
		"a refusal":           http.StatusForbidden,
		"a bad token":         http.StatusUnauthorized,
		"the service is up":   http.StatusTooManyRequests,
		"the service is down": http.StatusBadGateway,
	} {
		t.Run(name, func(t *testing.T) {
			server := newStub()
			server.status = status
			httpServer := httptest.NewServer(server)
			defer httpServer.Close()
			_, err := mustStore(t, config.Secrets{
				Adapter: "openbao", URL: httpServer.URL, Token: "a-token-never-quoted",
			}).Get(context.Background(), secrets.Shared("mail.password"))
			if err == nil {
				t.Fatal("a refused read answered nothing")
			}
			if errors.Is(err, secrets.ErrNotFound) && status != http.StatusForbidden {
				t.Errorf("a %d answered ErrNotFound: %v", status, err)
			}
			if strings.Contains(err.Error(), "a-token-never-quoted") {
				t.Errorf("the refusal carries the token: %v", err)
			}
		})
	}
}

// TestASoftDeletedVersionIsAMiss is the KV v2 case a reader cannot see: a deleted
// version answers 200 with the bytes still inside it, and whoever deleted it meant it.
func TestASoftDeletedVersionIsAMiss(t *testing.T) {
	server := newStub()
	server.secrets["mail.password"] = stubSecret{value: "gone", version: 3, deleted: "2026-10-17T10:00:00Z"}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	_, err := mustStore(t, config.Secrets{Adapter: "openbao", URL: httpServer.URL, Token: "x"}).
		Get(context.Background(), secrets.Shared("mail.password"))
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("a soft-deleted version = %v; want ErrNotFound", err)
	}
}

// TestTheTokenIsNeverInASentence is the redaction case the valkey adapter takes: an
// unreachable secret service is named by its address, and the credential that would
// have opened it is not in the line an operator reads.
func TestTheTokenIsNeverInASentence(t *testing.T) {
	const token = "s.openbao-token-never-quoted"
	_, err := mustStore(t, config.Secrets{
		Adapter: "openbao", URL: "http://127.0.0.1:1", Token: token,
	}).Get(context.Background(), secrets.Shared("mail.password"))
	if err == nil {
		t.Fatal("a service behind a closed port answered")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("the refusal carries the token: %v", err)
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("the refusal does not name the service it could not reach: %v", err)
	}
}

// TestTheContainerPinIsWhereTheSkipSaysItIs refuses the quiet failure: if the
// service leaves compose.yaml, the Makefile stops exporting its address, or the stack
// stops mounting the two engines the cases read, then either the one real-server run
// of this adapter skips in every check and every CI job and the skip sentence sends
// the reader to a command that starts nothing, or it runs against a server that
// answers "no mount point" for half the case list.
func TestTheContainerPinIsWhereTheSkipSaysItIs(t *testing.T) {
	compose, err := os.ReadFile("../../../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	if !regexp.MustCompile(`(?m)^  openbao:\s*$`).Match(compose) {
		t.Error("compose.yaml declares no openbao service, so `make up` starts nothing for this suite to read")
	}
	makefile, err := os.ReadFile("../../../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	if !bytes.Contains(makefile, []byte("PLATFORMKIT_TEST_OPENBAO_URL")) {
		t.Error("the Makefile never sets PLATFORMKIT_TEST_OPENBAO_URL, so `make check` skips the OpenBao adapter's conformance run")
	}
	// The two mounts are half of what this suite reads, and only the stack's own
	// start-up makes them: dev mode mounts KV v2 and not transit, so a service that
	// lost its mount step would answer the sealing cases as a broken adapter.
	if !bytes.Contains(compose, []byte("openbao-dev.sh")) {
		t.Error("compose.yaml's openbao service mounts nothing, so the transit cases have no engine to run against")
	}
	for _, enable := range []string{
		"bao secrets enable -path=secret kv-v2",
		"bao secrets enable -path=transit transit",
	} {
		if !bytes.Contains(script(t), []byte(enable)) {
			t.Errorf("the stack's mount step never runs %q", enable)
		}
	}
}

// script is the dev-mode OpenBao's own start-up, read from where compose.yaml mounts
// it: a fixture a test cannot read is a fixture whose promises nobody checks.
func script(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../../deploy/openbao-dev.sh")
	if err != nil {
		t.Fatalf("read the dev-mode OpenBao's start-up script: %v", err)
	}
	return raw
}

// TestTheSharedSuiteAgainstTheContainer is the same case list against a real
// OpenBao, and the answer to a stub that agrees with the client it is testing. The
// mount table, the two key types and what a KV v2 `value` can hold byte for byte are
// the three things only a server can say, so this run says them from one.
func TestTheSharedSuiteAgainstTheContainer(t *testing.T) {
	url, token := os.Getenv("PLATFORMKIT_TEST_OPENBAO_URL"), os.Getenv("PLATFORMKIT_TEST_OPENBAO_TOKEN")
	if url == "" || token == "" {
		t.Skip("PLATFORMKIT_TEST_OPENBAO_URL and PLATFORMKIT_TEST_OPENBAO_TOKEN name the dev-mode OpenBao `make up` starts; neither is set, so this run answers nothing and the wire cases above are the only evidence about this adapter")
	}
	// The two mounts are named here rather than left empty: the fixture addresses the
	// server directly, so an empty name would be a path with a hole in it, and these
	// are the names New falls back to when a deployment names none.
	cfg := config.Secrets{
		Adapter: "openbao", URL: url, Token: token,
		KVMount: openbao.DefaultKVMount, TransitMount: openbao.DefaultTransitMount,
	}
	client := &http.Client{Timeout: 10 * time.Second}
	adapter := secretstest.Adapter{
		Name:  "openbao",
		Store: func(*testing.T) secrets.Store { return mustStore(t, cfg) },
		Failing: func(*testing.T) secrets.Store {
			return mustStore(t, config.Secrets{Adapter: "openbao", URL: "http://127.0.0.1:1", Token: token})
		},
		Write: func(t *testing.T, ref secrets.Ref, plaintext []byte) {
			t.Helper()
			// The mount is the operator's act, not the port's: this is the request a
			// `vault kv put` sends, and it is here because the suite needs a
			// rotation it can cause.
			put(t, client, url, token, cfg.KVMount+"/data/"+pathOf(ref),
				map[string]any{"data": map[string]string{"value": string(plaintext)}})
		},
		WriteKey: func(t *testing.T, ref secrets.Ref) {
			t.Helper()
			// Mounting a key is the same kind of act, and `derived` is the flag this
			// adapter's tenant binding reads: a customer's key must be derived for its
			// tenant context to mean anything to the server, and the installation's own
			// must not be, because a derived key refuses a call that carries no context.
			put(t, client, url, token, cfg.TransitMount+"/keys/"+openbao.KeyAddress(ref), map[string]any{
				"type": "aes256-gcm96", "derived": ref.Tenant() != uuid.Nil,
			})
		},
		Exact: []byte("hunter2\n\u00e9\u4e2d\u0000"),
	}
	secretstest.Conformance(t, adapter)
	secretstest.Tenants(t, adapter)
	secretstest.Versions(t, adapter)
	secretstest.Keys(t, adapter)
	secretstest.TenantKeys(t, adapter)
}

// put sends one JSON body to one path of the server's API with the token in the
// header the server reads, and fails the case rather than returning an error: a
// fixture this suite could not build is not a case worth running.
func put(t *testing.T, client *http.Client, url, token, path string, body any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url+"/v1/"+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Vault-Token", token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("mounting %s: %v", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		t.Fatalf("mounting %s: %s", path, response.Status)
	}
}

// TestATransitContextIsNotOptionalToTheKey says the derivation is the server's and
// this adapter only carries it: a seal made with no context (the installation's own
// key) is a different request from one made with a tenant's, and a fake that ignored
// the field would pass every case above with the binding missing.
func TestATransitContextIsNotOptionalToTheKey(t *testing.T) {
	server := newStub()
	server.keys["suite-key"] = []byte(secretstest.KeyMaterial)
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	store := mustStore(t, config.Secrets{Adapter: "openbao", URL: httpServer.URL, Token: "x"})

	sealed, err := store.Seal(context.Background(), secrets.Shared("suite-key"), []byte("a deployment's own"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if regexp.MustCompile(`^vault:v\d+:`).MatchString("") || !regexp.MustCompile(`^vault:v\d+:`).Match(sealed) {
		t.Errorf("the envelope is %q; want the key service's own spelling, verbatim", sealed)
	}
	if _, err := store.Open(tenantContext(tenantB), secrets.Of(tenantB, "suite-key"), sealed); err == nil {
		t.Error("a ciphertext written with no derivation context opened with a tenant's")
	}
}

func tenantContext(tenant uuid.UUID) context.Context {
	return tenancy.WithTenant(context.Background(), tenancy.Tenant{ID: tenant, Slug: "tenant-" + tenant.String()[:8]})
}

func mustStore(t *testing.T, cfg config.Secrets) secrets.Store {
	t.Helper()
	store, err := openbao.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store
}

func pathOf(ref secrets.Ref) string {
	if ref.Tenant() == uuid.Nil {
		return ref.Name()
	}
	return ref.Tenant().String() + "/" + ref.Name()
}

var _ = time.Second
