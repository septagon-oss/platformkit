package filetest

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// ScopeFor is how a test mints a Scope: the same door a request uses, given a
// context carrying the tenant the test is pretending to be. There is no
// fourth door, and the suite does not get one — if the suite could build a
// Scope for a tenant nothing minted, so could a caller, and the type would be
// decoration.
func ScopeFor(ctx context.Context, tenant uuid.UUID) (contracts.Scope, error) {
	return contracts.ScopeOf(tenancy.WithTenant(ctx, tenancy.Tenant{ID: tenant}))
}

// TenantScope is ScopeFor over a fresh context, for the cases that name a
// tenant and nothing else.
func TenantScope(t *testing.T, tenant uuid.UUID) contracts.Scope {
	t.Helper()
	s, err := ScopeFor(context.Background(), tenant)
	if err != nil {
		t.Fatalf("mint a scope for %s: %v", tenant, err)
	}
	return s
}

// Memory is contracts.Storage over a map, for a consumer — and for the
// conformance suite — that wants a file module without a disk. It keeps the
// same promises the disk one does: a key that already exists is refused, a key
// with nothing at it is not an error to delete, and Get answers ErrNoBlob.
//
// The map is keyed by tenant and key together. That is the whole of what makes
// it a double of the tenant-typed port rather than a double of the old one: an
// implementation that ignored the scope it was handed would keep answering Get
// for the same key under every tenant, which is exactly the bug RunStorage's
// "a scope names one tenant's prefix" case exists to catch.
type Memory struct {
	mu    sync.Mutex
	blobs map[string][]byte
	// written records when each blob arrived, which is the only way this store
	// can answer a listing with a cutoff instead of ignoring one — and a
	// double that ignored the cutoff would let a sweep that forgot it pass.
	written map[string]time.Time
}

// NewMemory returns an empty store.
func NewMemory() *Memory {
	return &Memory{blobs: map[string][]byte{}, written: map[string]time.Time{}}
}

var _ contracts.Storage = (*Memory)(nil)

// at is the map key: the tenant's prefix and the key, joined the way every
// adapter joins them, so the weak double answers to the same containment rule.
func at(s contracts.Scope, k contracts.Key) (string, error) { return s.ObjectName(k) }

// Put reads everything and keeps it. size is ignored, as it is on disk, and so
// is meta, for the same reason Local gives.
func (m *Memory) Put(_ context.Context, s contracts.Scope, k contracts.Key, r io.Reader, _ int64, _ contracts.Meta) error {
	name, err := at(s, k)
	if err != nil {
		return err
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("filetest: read %s: %w", k, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, taken := m.blobs[name]; taken {
		return fmt.Errorf("filetest: %s is already stored", name)
	}
	m.blobs[name] = body
	m.written[name] = time.Now()
	return nil
}

// Get opens the bytes, or ErrNoBlob.
func (m *Memory) Get(_ context.Context, s contracts.Scope, k contracts.Key) (io.ReadCloser, error) {
	name, err := at(s, k)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	body, ok := m.blobs[name]
	if !ok {
		return nil, contracts.ErrNoBlob
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

// Delete removes the bytes, and a key with nothing at it is not an error.
func (m *Memory) Delete(_ context.Context, s contracts.Scope, k contracts.Key) error {
	name, err := at(s, k)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.blobs, name)
	delete(m.written, name)
	return nil
}

// Blobs and RemoveBlob make the memory store one the reconciliation cases run
// against. Every blob knows its tenant, because the map key carries it, which
// is more than the disk one can say about a directory it flattened.
func (m *Memory) Blobs(_ context.Context, _ db.Tx[db.System], before time.Time) ([]contracts.Blob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []contracts.Blob
	for name, at := range m.written {
		if !at.Before(before) {
			continue
		}
		tenant, key, ok := strings.Cut(name, "/")
		if !ok {
			continue
		}
		id, err := uuid.Parse(tenant)
		if err != nil {
			continue
		}
		out = append(out, contracts.Blob{TenantID: id, Key: contracts.Key(key)})
	}
	slices.SortFunc(out, func(a, b contracts.Blob) int {
		return cmp.Compare(a.TenantID.String()+a.Key.String(), b.TenantID.String()+b.Key.String())
	})
	return out, nil
}

func (m *Memory) RemoveBlob(_ context.Context, _ db.Tx[db.System], b contracts.Blob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := b.TenantID.String() + "/" + b.Key.String()
	delete(m.blobs, name)
	delete(m.written, name)
	return nil
}

// Prove reports whether anything is left at a name, which for a map is the
// trivially honest answer: it counts what is in it.
func (m *Memory) Prove(_ context.Context, s contracts.Scope, k contracts.Key) (int, error) {
	name, err := at(s, k)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.blobs[name]; ok {
		return 1, nil
	}
	return 0, nil
}

var (
	_ contracts.Reconciler = (*Memory)(nil)
	_ contracts.Prover     = (*Memory)(nil)
)

// Keys is every tenant-and-key the store holds, sorted. It is the one question
// contracts.Storage does not answer and the only way a test can see an orphan.
func (m *Memory) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Sorted(maps.Keys(m.blobs))
}
