package filetest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// Memory is contracts.Storage over a map, for a consumer — and for the
// conformance suite — that wants a file module without a disk. It keeps the
// same promises the disk one does: a key that already exists is refused, a key
// with nothing at it is not an error to delete, and Get answers ErrNoBlob.
type Memory struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

// NewMemory returns an empty store.
func NewMemory() *Memory { return &Memory{blobs: map[string][]byte{}} }

var _ contracts.Storage = (*Memory)(nil)

// Put reads everything and keeps it. size is ignored, as it is on disk.
func (m *Memory) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("filetest: read %s: %w", key, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, taken := m.blobs[key]; taken {
		return fmt.Errorf("filetest: %s is already stored", key)
	}
	m.blobs[key] = body
	return nil
}

// Get opens the bytes, or ErrNoBlob.
func (m *Memory) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	body, ok := m.blobs[key]
	if !ok {
		return nil, contracts.ErrNoBlob
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

// Delete removes the bytes, and a key with nothing at it is not an error.
func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.blobs, key)
	return nil
}

// Keys is every key the store holds, sorted. It is the one question
// contracts.Storage does not answer, and the only way a test can see an orphan.
func (m *Memory) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Sorted(maps.Keys(m.blobs))
}
