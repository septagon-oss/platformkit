package internal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// Local is contracts.Storage on the filesystem, which is what a laptop, a
// single machine and a mounted volume all are. The implementations that speak
// to an object store live outside this repository.
//
// A key is a UUID, checked here as well as generated here, and that is the
// whole path-traversal argument: there is no caller-supplied component in a key
// to escape a directory with, and the check makes that true of a caller this
// package cannot see. The first two characters are a subdirectory, because a
// directory with a million entries is slow in every filesystem worth naming.
type Local struct{ dir string }

// NewLocal returns storage under dir. The directory is created when the first
// blob is written rather than here, so constructing this in a composition
// touches no disk.
func NewLocal(dir string) *Local { return &Local{dir: dir} }

var _ contracts.Storage = (*Local)(nil)

// key is a UUID, lower case, and nothing else.
var key = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// path is where the bytes for a key live, or an error for a key this package
// did not mint.
func (l *Local) path(k string) (string, error) {
	if !key.MatchString(k) {
		return "", fmt.Errorf("file: %q is not a storage key; a key is a UUID", k)
	}
	return filepath.Join(l.dir, k[:2], k), nil
}

// Put writes the bytes, refusing a key that already exists: a key is minted per
// upload, so a collision is a bug rather than a replacement. size is ignored —
// a filesystem needs no length up front.
func (l *Local) Put(_ context.Context, k string, r io.Reader, _ int64) error {
	at, err := l.path(k)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(at), 0o700); err != nil {
		return fmt.Errorf("file: make %s: %w", filepath.Dir(at), err)
	}
	f, err := os.OpenFile(at, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("file: create %s: %w", at, err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		// What was written is not a file anybody will find, because the row
		// that would have named it is not written either.
		_ = os.Remove(at)
		return fmt.Errorf("file: write %s: %w", at, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(at)
		return fmt.Errorf("file: close %s: %w", at, err)
	}
	return nil
}

// Get opens the bytes, or ErrNoBlob when there are none.
func (l *Local) Get(_ context.Context, k string) (io.ReadCloser, error) {
	at, err := l.path(k)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(at)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, contracts.ErrNoBlob
	}
	if err != nil {
		return nil, fmt.Errorf("file: open %s: %w", at, err)
	}
	return f, nil
}

// Delete removes the bytes. A key with nothing at it is not an error: the
// worker that calls this retries, and a retry that failed because the first
// attempt succeeded would never stop.
func (l *Local) Delete(_ context.Context, k string) error {
	at, err := l.path(k)
	if err != nil {
		return err
	}
	if err := os.Remove(at); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("file: remove %s: %w", at, err)
	}
	return nil
}
