// Package secretstest supplies shared secrets.Store conformance cases.
// Adapter.Exact declares byte preservation, and callers select the suites their
// store supports, naming unsupported capabilities in their own package.
package secretstest

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/secrets"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// tenantA and tenantB are two customers, fixed bytes rather than fresh uuids so a
// store that lets one reach the other is reproducible from the failure message.
var (
	tenantA = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	tenantB = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

// Store returns one handle onto the store under test.
//
// Calling it twice inside one case must return two handles onto **one** store:
// that is what two replicas of one deployment means here. A store whose factory
// builds a fresh one per call cannot pass the sharing cases, which is part of what
// this suite is for.
type Store func(t *testing.T) secrets.Store

// Failing returns one handle onto a store that cannot be reached — no process
// listening, a source that failed, a directory that is gone — and answers with an
// error within a bounded time rather than hanging.
type Failing func(t *testing.T) secrets.Store

// WriteKey provisions a key under an address, which the port cannot do either:
// a key is mounted by a deployment, not minted by a reader. Each envelope case asks
// for one under suiteKey before it runs, so an adapter's fixture is about where keys
// live rather than about this suite's ordering.
type WriteKey func(t *testing.T, ref secrets.Ref)

// SuiteKey is the address the envelope cases seal under. It is exported because an
// adapter's WriteKey has to provision that name and nothing else.
const SuiteKey = "suite-key"

// SuiteKeyOther is the second key the binding cases need: a name the store can read,
// so the refusal is about the address and not about a name nobody mounted.
const SuiteKeyOther = "suite-key-other"

// Write puts plaintext under an address, and is what a read-only port still needs:
// the port has no write method because a secret arrives from outside the process,
// so the suite cannot create its own fixture through the interface it is testing.
// Every adapter has one at test time — env sets a variable, file writes a file, the
// KV store takes its own PUT, and the map's map is the caller's.
type Write func(t *testing.T, ref secrets.Ref, plaintext []byte)

// Adapter is one store, and the three things a case needs to talk to it.
type Adapter struct {
	Name     string
	Store    Store
	Failing  Failing
	Write    Write
	WriteKey WriteKey
	// Exact is the bytes this store must return unchanged, and it is the store's
	// own claim: a KV store that cannot hold a 0xff byte says so here rather than
	// dropping the byte-for-byte case, and the case then tests everything it can.
	Exact []byte
}

// Conformance runs the cases every adapter answers: a miss, the bytes, the refusal
// of a name, and a store that has stopped answering.
func Conformance(t *testing.T, a Adapter) {
	t.Helper()

	// C1. A miss is ErrNotFound and an outage is anything else. An adapter that
	// answers a miss as an error makes every cold store look dead to a caller that
	// fails open, and one that answers an outage as a miss loses the secret silently
	// and serves a request with no credential at all — the worse of the two.
	t.Run("a miss is not an outage", func(t *testing.T) {
		c := a.Store(t)
		_, err := c.Get(context.Background(), secrets.Shared("absent-name"))
		if !errors.Is(err, secrets.ErrNotFound) {
			t.Errorf("Get of an absent name = %v; want ErrNotFound", err)
		}
		broken := a.Failing(t)
		_, err = broken.Get(context.Background(), secrets.Shared("absent-name"))
		if err == nil {
			t.Fatal("an unreachable store answered as if it had read something")
		}
		if errors.Is(err, secrets.ErrNotFound) {
			t.Errorf("an unreachable store answered ErrNotFound; a miss and an outage are one answer only by a bug: %v", err)
		}
	})

	// C2. Byte for byte, including the bytes a JSON string, a base64 round trip or
	// a strings.TrimSpace would quietly corrupt: a password that comes back
	// different is a password that stops working with no explanation anywhere.
	t.Run("a secret is read back byte for byte", func(t *testing.T) {
		a.Write(t, secrets.Shared("bytes"), a.Exact)
		got, err := a.Store(t).Get(context.Background(), secrets.Shared("bytes"))
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !bytes.Equal(got.Bytes, a.Exact) {
			t.Errorf("Get = % x; want % x", got.Bytes, a.Exact)
		}
	})

	// C3. A name the grammar refuses is refused before anything is read, by every
	// adapter. file runs this against a real directory, so a traversal that reached
	// outside it would leave evidence rather than a comment.
	t.Run("a name this port cannot address is refused", func(t *testing.T) {
		c := a.Store(t)
		bad := map[string]secrets.Ref{
			"../escape":    secrets.Shared("../escape"),
			"a/b":          secrets.Shared("a/b"),
			".hidden":      secrets.Shared(".hidden"),
			"empty":        secrets.Shared(""),
			"MiXeD":        secrets.Shared("MiXeD"),
			"64 bytes":     secrets.Shared(string(bytes.Repeat([]byte("a"), 64))),
			"a trailing .": secrets.Shared("trailing."),
			`a backslash`:  secrets.Shared(`a\b`),
			"a/../b":       secrets.Shared("a/../b"),
			"a zero Ref":   secrets.Ref{},
			"a nil tenant": secrets.Of(uuid.Nil, "ok"),
		}
		for name, ref := range bad {
			if _, err := c.Get(context.Background(), ref); !errors.Is(err, secrets.ErrBadName) {
				t.Errorf("Get of %s = %v; want ErrBadName and nothing read", name, err)
			}
			if _, err := c.Version(context.Background(), ref); !errors.Is(err, secrets.ErrBadName) {
				t.Errorf("Version of %s = %v; want ErrBadName", name, err)
			}
			if _, err := c.Seal(context.Background(), ref, []byte("x")); !errors.Is(err, secrets.ErrBadName) {
				t.Errorf("Seal under %s = %v; want ErrBadName", name, err)
			}
		}
	})

	// C4. A read of a bad address reads nothing. The zero value is what a caller
	// that ignored Of's refusal hands along, and a store that answered it would
	// answer it for every name at once.
	t.Run("an unreachable store answers within its caller's deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		start := time.Now()
		if _, err := a.Failing(t).Get(ctx, secrets.Shared("anything")); err == nil {
			t.Fatal("an unreachable store answered nothing")
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("an unreachable store took %s to say no; a caller on the request path needs an error, not a hang", elapsed)
		}
	})
}

// Tenants runs the cases a store answers only if it can hold a tenant's secret: the
// two address spaces never seeing each other, and a read refused for a context that
// resolved somebody else. env does not run it — it refuses a tenant's address, which
// its own package asserts as the answer rather than as an absent test.
func Tenants(t *testing.T, a Adapter) {
	t.Helper()

	// C5. A shared name and a tenant's name are different addresses, and a layout
	// that folds them together answers the installation's password to a customer's
	// request. Both directions, because the collapse can go either way.
	t.Run("a shared name and a tenant's name never see each other", func(t *testing.T) {
		a.Write(t, secrets.Shared("folded"), []byte("installation"))
		a.Write(t, secrets.Of(tenantA, "folded"), []byte("customer"))
		c := a.Store(t)

		shared, err := c.Get(tenantContext(tenantA), secrets.Shared("folded"))
		if err != nil || string(shared.Bytes) != "installation" {
			t.Errorf("the installation's name answered %q, %v; want the installation's bytes", shared.Bytes, err)
		}
		own, err := c.Get(tenantContext(tenantA), secrets.Of(tenantA, "folded"))
		if err != nil || string(own.Bytes) != "customer" {
			t.Errorf("a tenant's name answered %q, %v; want its own bytes", own.Bytes, err)
		}
		if _, err := c.Get(tenantContext(tenantB), secrets.Of(tenantA, "folded")); !errors.Is(err, secrets.ErrNoScope) {
			t.Errorf("tenant B reading tenant A's name = %v; want ErrNoScope", err)
		}
	})

	// C6. The scope rule, all four corners: the right tenant is allowed, the wrong
	// one is refused without a read, no tenant at all may reach a customer's secret,
	// and the installation's own secret is readable either way — a rule, not a
	// permission the port grants, which stays the caller's command's.
	t.Run("a read carries the tenant that may make it", func(t *testing.T) {
		a.Write(t, secrets.Of(tenantA, "mail.password"), []byte("customer"))
		a.Write(t, secrets.Shared("mail.password"), []byte("installation"))
		c := a.Store(t)

		if _, err := c.Get(tenantContext(tenantA), secrets.Of(tenantA, "mail.password")); err != nil {
			t.Errorf("a tenant reading its own secret: %v", err)
		}
		if _, err := c.Get(tenantContext(tenantB), secrets.Of(tenantA, "mail.password")); !errors.Is(err, secrets.ErrNoScope) {
			t.Errorf("a tenant reading another's secret = %v; want ErrNoScope and no bytes read", err)
		}
		if _, err := c.Get(context.Background(), secrets.Of(tenantA, "mail.password")); !errors.Is(err, secrets.ErrNoScope) {
			t.Errorf("a context that resolved no tenant reading a tenant's secret = %v; want ErrNoScope", err)
		}
		for name, ctx := range map[string]context.Context{
			"a tenant's context": tenantContext(tenantB),
			"no context at all":  context.Background(),
		} {
			if _, err := c.Get(ctx, secrets.Shared("mail.password")); err != nil {
				t.Errorf("the installation's secret read from %s: %v", name, err)
			}
		}
	})
}

// Versions runs the rotation case, for the stores that have a token to move. env
// answers no version and runs no case here; its own package asserts the empty
// answer, which is the same statement made positively.
func Versions(t *testing.T, a Adapter) {
	t.Helper()

	// C7. Version is stable across reads of the same bytes and moves when the bytes
	// move. A token that is random invalidates a caller's cache forever; a constant
	// hides a rotation, which is the one thing a version is for.
	t.Run("a version moves when the bytes move", func(t *testing.T) {
		ref := secrets.Shared("rotating")
		a.Write(t, ref, []byte("first"))
		c := a.Store(t)
		first, err := c.Version(context.Background(), ref)
		if err != nil {
			t.Fatalf("Version: %v", err)
		}
		if again, err := c.Version(context.Background(), ref); err != nil || again != first {
			t.Errorf("Version of unchanged bytes = %q, %v; want %q again", again, err, first)
		}
		a.Write(t, ref, []byte("second"))
		after, err := c.Version(context.Background(), ref)
		if err != nil {
			t.Fatalf("Version after a rotation: %v", err)
		}
		if after == first {
			t.Errorf("Version answered %q for different bytes; a token that never moves hides a rotation", after)
		}
	})
}

// TenantKeys runs the envelope case that needs a tenant's key, which the stores
// that cannot hold a tenant's secret cannot run: env refuses the address itself, and
// asserting that refusal is its own package's job, not a skip here.
func TenantKeys(t *testing.T, a Adapter) {
	t.Helper()
	plaintext := []byte("a credential worth sealing")

	// A seal carries the address it was written for, tenant included, so a
	// ciphertext taken out of one customer's column does not open as another
	// customer's — not even for a request that legitimately belongs to tenant B,
	// which is the case a scope check alone cannot make.
	t.Run("one tenant's seal does not open as another's", func(t *testing.T) {
		c := a.Store(t)
		a.WriteKey(t, secrets.Of(tenantA, SuiteKey))
		a.WriteKey(t, secrets.Of(tenantB, SuiteKey))
		underTenant, err := c.Seal(tenantContext(tenantA), secrets.Of(tenantA, SuiteKey), plaintext)
		if err != nil {
			t.Fatalf("Seal for a tenant: %v", err)
		}
		if _, err := c.Open(tenantContext(tenantB), secrets.Of(tenantB, SuiteKey), underTenant); !errors.Is(err, secrets.ErrSealed) {
			t.Errorf("a tenant opening another tenant's seal = %v; want ErrSealed", err)
		}
		if _, err := c.Open(tenantContext(tenantA), secrets.Of(tenantA, SuiteKey), underTenant); err != nil {
			t.Errorf("its own tenant opening the seal: %v", err)
		}
	})

	// The port's name grammar admits a shared name that reads as a customer's key
	// address: a UUID, a hyphen and a name are legal name bytes, and a shared address
	// is open to every context. A store that folds the two address spaces onto one
	// handle hands that customer's key to a request which resolved somebody else, so
	// the two spellings have to be two keys wherever a key lives. What this case can
	// prove of a store holding its own material is the address inside the envelope; a
	// store behind a key service proves it of the service only against a server, and
	// the OpenBao adapter's container case is the run that does that.
	t.Run("a shared name that spells a tenant's address is not that address", func(t *testing.T) {
		c := a.Store(t)
		spelled := secrets.Shared(tenantA.String() + "-" + SuiteKey)
		a.WriteKey(t, secrets.Of(tenantA, SuiteKey))
		a.WriteKey(t, spelled)

		underTenant, err := c.Seal(tenantContext(tenantA), secrets.Of(tenantA, SuiteKey), plaintext)
		if err != nil {
			t.Fatalf("a tenant sealing under its own key: %v", err)
		}
		if got, err := c.Open(tenantContext(tenantB), spelled, underTenant); err == nil || len(got) != 0 {
			t.Errorf("tenant B opening tenant A's seal under %s = %q, %v; want a refusal and no bytes", spelled, got, err)
		}
		underShared, err := c.Seal(context.Background(), spelled, plaintext)
		if err != nil {
			t.Fatalf("sealing under %s: %v", spelled, err)
		}
		if _, err := c.Open(tenantContext(tenantA), secrets.Of(tenantA, SuiteKey), underShared); !errors.Is(err, secrets.ErrSealed) {
			t.Errorf("a tenant opening the shared spelling's seal = %v; want ErrSealed", err)
		}
		if _, err := c.Open(context.Background(), spelled, underShared); err != nil {
			t.Errorf("opening the shared spelling's own seal: %v", err)
		}
	})
}

// otherKeySeal is a seal made under a different key of the same width, which is the
// case an adapter with one key in its fixture cannot produce by itself.
func otherKeySeal(t *testing.T, plaintext []byte) []byte {
	t.Helper()
	other := secrets.Map(nil, map[secrets.Ref][]byte{secrets.Shared(SuiteKey): []byte(OtherKeyMaterial)})
	sealed, err := other.Seal(context.Background(), secrets.Shared(SuiteKey), plaintext)
	if err != nil {
		t.Fatalf("sealing under the second key: %v", err)
	}
	return sealed
}

// KeyMaterial is one AES-256 key in the spelling every local store keeps one in:
// base64.StdEncoding of 32 bytes.
const KeyMaterial = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

// OtherKeyMaterial is a second one, exported because the case that needs a seal made
// under a key the store under test does not hold has to build it, and a fixture that
// mounted the same bytes under two names would test nothing about keys at all.
const OtherKeyMaterial = "ZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXp7fH1+f4CBgoM="

// tenantContext is a context that resolved one customer, which is the half of a read
// the port's scope check reads. A slug is filled in because tenancy.Tenant's is
// written by every real resolution, and a span with no slug on it is a difference
// this suite would not want to be the first to notice.
func tenantContext(tenant uuid.UUID) context.Context {
	return tenancy.WithTenant(context.Background(), tenancy.Tenant{
		ID: tenant, Slug: "tenant-" + tenant.String()[:8],
	})
}
