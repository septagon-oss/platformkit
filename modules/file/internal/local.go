package internal

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// Local is contracts.Storage on the filesystem, which is what a laptop, a
// single machine and a mounted volume all are.
//
// A key is a UUID, checked here as well as generated here, and a tenant is a
// UUID the scope carried, checked the same way: there is no caller-supplied
// component in either path to escape a directory with, and the checks make that
// true of a caller this package cannot see.
//
// Bytes live under the tenant's own directory — dir/<tenant>/<first 2 hex of
// key>/<key> — for one reason and not two. It is not the containment, which the
// key check already is: it is that a tenant's bytes are then a directory, which
// is what makes "copy this tenant out" and "list what this tenant holds"
// operations a walk does rather than a scan of a million files that name nobody.
// The one exception is a migration's: an installation that predates the scope
// has bytes in dir/<2 hex>/<key>, and Read still finds those, because a store
// that cannot read what it wrote is not a store.
type Local struct{ dir string }

// NewLocal returns storage under dir. The directory is created when the first
// blob is written rather than here, so constructing this in a composition
// touches no disk.
func NewLocal(dir string) *Local { return &Local{dir: dir} }

var _ contracts.Storage = (*Local)(nil)

// at is where the bytes for a scope and key live, or an error for either this
// package could not have minted.
func (l *Local) at(s contracts.Scope, k contracts.Key) (string, error) {
	name, err := s.ObjectName(k)
	if err != nil {
		return "", err
	}
	// ObjectName joined the two with a slash; split it back to put the
	// subdirectory in front of the key and not in front of the tenant.
	tenant, key := filepath.ToSlash(name)[:36], name[37:]
	return filepath.Join(l.dir, tenant, key[:2], key), nil
}

// legacy is where Put wrote these bytes before the port carried a scope, which
// a Get still reads so an installation's existing uploads keep working.
func (l *Local) legacy(k contracts.Key) string {
	return filepath.Join(l.dir, k.String()[:2], k.String())
}

// Put writes the bytes, refusing a key that already exists: a key is minted per
// upload, so a collision is a bug rather than a replacement. size is ignored — a
// filesystem needs no length up front — and so is meta, which is the honest
// answer for a filesystem: a byte served out of here is served by this process,
// and response.go is where its headers are written.
func (l *Local) Put(_ context.Context, s contracts.Scope, k contracts.Key, r io.Reader, _ int64, _ contracts.Meta) error {
	at, err := l.at(s, k)
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
func (l *Local) Get(_ context.Context, s contracts.Scope, k contracts.Key) (io.ReadCloser, error) {
	at, err := l.at(s, k)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(at)
	if errors.Is(err, fs.ErrNotExist) {
		if f, err = os.Open(l.legacy(k)); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, contracts.ErrNoBlob
			}
			return nil, fmt.Errorf("file: open %s: %w", at, err)
		}
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("file: open %s: %w", at, err)
	}
	return f, nil
}

// Delete removes the bytes in this tenant's directory and the ones the flat
// layout wrote under the same key. A key with nothing at it is not an error: the
// worker that calls this retries, and a retry that failed because the first
// attempt succeeded would never stop.
func (l *Local) Delete(_ context.Context, s contracts.Scope, k contracts.Key) error {
	at, err := l.at(s, k)
	if err != nil {
		return err
	}
	if err := os.Remove(at); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("file: remove %s: %w", at, err)
	}
	if err := os.Remove(l.legacy(k)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("file: remove %s: %w", l.legacy(k), err)
	}
	return nil
}

// remove is Delete for a tenant the caller names directly, which is the one
// thing a sweep may do and a request may not: it is reached only through
// Reconciler.RemoveBlob, whose transaction argument is a db.Tx[db.System], and it
// still composes the path from two validated UUIDs rather than from a name.
func (l *Local) remove(tenant uuid.UUID, k contracts.Key) error {
	if _, err := contracts.ParseKey(k.String()); err != nil {
		return err
	}
	if tenant == uuid.Nil {
		return errors.New("file: remove needs the tenant the object lives under")
	}
	at := filepath.Join(l.dir, tenant.String(), k.String()[:2], k.String())
	if err := os.Remove(at); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("file: remove %s: %w", at, err)
	}
	return nil
}

// Blobs is contracts.Reconciler on the filesystem: every object written before
// before, each with the tenant whose directory it is in.
//
// It walks the tenant directories and reads each entry's modification time, which
// is when the upload finished writing it. A directory that is not a tenant id,
// and a file that is not a key, are skipped: they are not something this package
// wrote, and a sweep that deleted what it did not recognise would be a sweep that
// deletes a backup somebody left here.
func (l *Local) Blobs(_ context.Context, _ db.Tx[db.System], before time.Time) ([]contracts.Blob, error) {
	var out []contracts.Blob
	err := filepath.WalkDir(l.dir, func(at string, e fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case e.IsDir():
			return nil
		}
		id, ok := minted(e.Name())
		if !ok {
			return nil // not a key this package wrote; see the note above
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if !info.ModTime().Before(before) {
			return nil
		}
		tenant, ok := l.tenantOf(at, id)
		if !ok {
			return nil
		}
		out = append(out, contracts.Blob{TenantID: tenant, Key: contracts.Key(id.String())})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("file: walk %s: %w", l.dir, err)
	}
	slices.SortFunc(out, func(a, b contracts.Blob) int {
		if c := cmp.Compare(a.TenantID.String(), b.TenantID.String()); c != 0 {
			return c
		}
		return cmp.Compare(a.Key, b.Key)
	})
	return out, nil
}

// tenantOf is whose object this is, from where it sits: <dir>/<tenant>/<2>/<key>
// is that tenant's, and the flat <dir>/<2>/<key> a release before the scope is
// nobody's that this package can name, so it reports uuid.Nil and the sweep
// below treats it as a blob no row claims.
func (l *Local) tenantOf(at string, key uuid.UUID) (uuid.UUID, bool) {
	rel, err := filepath.Rel(l.dir, at)
	if err != nil {
		return uuid.Nil, false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	switch len(parts) {
	case 3:
		if parts[2] != key.String() || parts[1] != key.String()[:2] {
			return uuid.Nil, false
		}
		return minted(parts[0])
	case 2:
		if parts[1] != key.String() || parts[0] != key.String()[:2] {
			return uuid.Nil, false
		}
		return uuid.Nil, true // written before the port carried a tenant
	}
	return uuid.Nil, false
}

var _ contracts.Reconciler = (*Local)(nil)

// minted is a name that is a UUID and nothing else — which is the same test for
// a tenant's directory as for a key's file, because both are UUIDs this module
// minted and neither is anything a caller wrote.
func minted(name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(name)
	return id, err == nil && id != uuid.Nil
}
