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

	"github.com/septagon-oss/platformkit/kit/appname"
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
// Bytes live in the owning tenant's own directory. It is not the containment,
// which the key check already is: it is that a tenant's bytes are then a
// directory, which is what makes "copy this tenant out" and "list what this
// tenant holds" operations a walk does rather than a scan of a million files
// that name nobody. An app that names itself gets one directory above that, its
// own slug, because a server hosts many apps over one mounted volume (decision
// 0074) and the tenant is only a label between two of them: without the segment
// two apps that minted the same tenant id would write one another's paths.
//
// Three positions are therefore in play, and every one of them is named by a
// constructor in kit/appname rather than spelled here:
//
//	<app>/<tenant>/<key>            an app that names itself writes here
//	<tenant>/<2 hex>/<key>          the deployment of one app, which has no slug
//	<2 hex>/<key>                   a release before the port carried a scope
//
// The older two are still read, because a store that cannot read what it wrote
// is not a store; the shard that the oldest layout needed — a flat directory of
// a million names is slow in every filesystem worth naming — has never been
// needed inside a tenant's own directory, and an operator who wants one moves
// the bytes rather than getting a second shape from this adapter.
//
// Reading is not sweeping: which of those positions a delete may reach is
// tenantOf's answer, and it is narrower than the list above on purpose.
type Local struct {
	dir string

	// app is the slug whose name every stored object's path carries; with the
	// tenant it is what keeps two apps sharing one volume from writing one path.
	// Empty is the deployment of one app, whose files sit where they were written
	// before this segment existed.
	app appname.Name
}

// NewLocal returns storage under dir. The directory is created when the first
// blob is written rather than here, so constructing this in a composition
// touches no disk.
func NewLocal(dir string) *Local { return NewLocalOf(appname.Name(""), dir) }

// NewLocalOf is NewLocal for a deployment that names its app: a stored object
// lands under <app>/<tenant>/<key> — the tenant is the one Scope names, never
// anything a caller wrote — so two apps on one mounted volume cannot write one
// another's bytes and an operator can point a quota, a bucket policy or a `du`
// at one app's bytes and then at one tenant's. The persisted key is still the
// caller's UUID.
//
// An installation that already holds bytes at the un-prefixed positions keeps
// reading them: Get, Delete and Prove look at every position below, so the move
// (`mv <dir>/<tenant>/<2>/<key> <dir>/<app>/<tenant>/<key>`, one move per blob)
// is a boot's step and not this adapter's, and a deployment that never takes it
// still serves what it holds. What it stops doing is reconciling those older
// bytes: Blobs lists this slug's own segment only, so an abandoned upload whose
// bytes were never moved stays on the volume until the move happens. That costs
// disk and deletes nothing, which is the right way to be wrong about a directory
// this app shares with every other app mounted at the same root.
func NewLocalOf(app appname.Name, dir string) *Local {
	return &Local{dir: dir, app: app}
}

// App is the slug whose segment these bytes sit in — the empty Name for the
// deployment of one app, whose bytes sit at the un-prefixed position every app on
// the volume can reach. The orphan sweep asks it (reconcile.go): the segment says
// which directories this adapter may list, and only a row says which app a tenant
// inside one of them belongs to.
func (l *Local) App() appname.Name { return l.app }

var _ contracts.Storage = (*Local)(nil)
var _ contracts.Prover = (*Local)(nil)
var _ contracts.Reconciler = (*Local)(nil)

// names is every position these bytes could sit at under this adapter's root,
// newest first, or an error for a scope and key this package could not have
// minted — a scope with no tenant in it among them, which is the refusal that
// keeps an app's blobs out of one directory shared by every tenant of it.
func (l *Local) names(s contracts.Scope, k contracts.Key) ([]string, error) {
	if _, err := s.ObjectName(k); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(k.String())
	if err != nil {
		return nil, fmt.Errorf("file: %q is not a storage key; a key is a UUID", k)
	}
	return l.positions(s.TenantID(), id), nil
}

// positions is those names for two UUIDs this package already believes: where a
// store that names its app writes, the same name with no app segment — the
// volume of a deployment that has not named its app yet, which an app that names
// itself still reads — and the flat directory from before the port carried a
// scope at all. Whichever of them holds the bytes, the key is one this module
// minted once, so at most one of them does.
func (l *Local) positions(tenant, id uuid.UUID) []string {
	names := []string{l.join(appname.StoragePath(l.app, tenant, id))}
	if l.app.Named() {
		names = append(names, l.join(appname.StoragePath(appname.Name(""), tenant, id)))
	}
	return append(names, l.join(appname.PreviousStoragePath(id)))
}

// join is the adapter's root with a name from kit/appname under it. One line
// spells that join, because the join of the root with a relative position is how
// a stored file's physical path is written, and the census counts the spelling
// (kit/appname/census_test.go): every position that reaches a path below comes
// from a constructor in that package, whichever of them the caller needed.
func (l *Local) join(rel string) string { return filepath.Join(l.dir, rel) }

// Put writes the bytes, refusing a key that already exists: a key is minted per
// upload, so a collision is a bug rather than a replacement. size is ignored — a
// filesystem needs no length up front — and so is meta, which is the honest
// answer for a filesystem: a byte served out of here is served by this process,
// and response.go is where its headers are written.
func (l *Local) Put(_ context.Context, s contracts.Scope, k contracts.Key, r io.Reader, _ int64, _ contracts.Meta) error {
	found, err := l.names(s, k)
	if err != nil {
		return err
	}
	at := found[0]
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

// Get opens the bytes, or ErrNoBlob when there are none. The first name a store
// holds them at wins, which is what lets an installation read what a previous
// release wrote while writing what it names now.
func (l *Local) Get(_ context.Context, s contracts.Scope, k contracts.Key) (io.ReadCloser, error) {
	names, err := l.names(s, k)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		f, err := os.Open(name)
		switch {
		case err == nil:
			return f, nil
		case errors.Is(err, fs.ErrNotExist):
			// Nothing at this name; the next one is a previous release's.
		default:
			// A read failure is an outage, and answering it as ErrNoBlob would
			// turn a broken volume into a tenant's missing files.
			return nil, fmt.Errorf("file: open %s: %w", name, err)
		}
	}
	return nil, contracts.ErrNoBlob
}

// Delete removes the bytes wherever this adapter has ever written them: the
// tenant's own directory under this app's segment, the same position with no app
// segment, and the flat layout from before the port carried a scope. A key with
// nothing at it is not an error: the worker that calls this retries, and a retry
// that failed because the first attempt succeeded would never stop.
func (l *Local) Delete(_ context.Context, s contracts.Scope, k contracts.Key) error {
	names, err := l.names(s, k)
	if err != nil {
		return err
	}
	return l.erase(names...)
}

// Prove is contracts.Prover on the filesystem, and the filesystem can answer it:
// a bucket with versioning switched on can hold copies a delete cannot reach, and
// a directory holds one file under one name or it holds nothing. The answer is
// therefore the honest one rather than the store's silence — and the difference
// matters, because a store that does not implement Prover leaves every erasure's
// verified_at NULL, so an installation running on disk would have no certified
// erasure ever, whatever the README promised about the certificate.
//
// Every name a delete wrote to is counted, for the same reason Delete removes
// all three: a key whose bytes are still lying in one older directory is a copy
// that is still here, and a certificate stamped over a delete that missed it
// would be a record that lied. Anything that is neither present nor absent — a
// broken volume, a directory in the place of a blob — is an error, because
// "cannot tell" answered as "gone" is the same lie in a different coat.
func (l *Local) Prove(_ context.Context, s contracts.Scope, k contracts.Key) (int, error) {
	names, err := l.names(s, k)
	if err != nil {
		return 0, err
	}
	seen := 0
	for _, candidate := range names {
		switch _, err := os.Stat(candidate); {
		case err == nil:
			seen++
		case errors.Is(err, fs.ErrNotExist):
			// Nothing at this name, which is the answer the certificate wants.
		case err != nil:
			return 0, fmt.Errorf("file: prove %s is gone: %w", candidate, err)
		}
	}
	return seen, nil
}

// remove is Delete for a tenant the caller names directly, which is the one
// thing a sweep may do and a request may not: it is reached only through
// Reconciler.RemoveBlob, whose transaction argument is a db.Tx[db.System], and it
// still composes each path from two validated UUIDs rather than from a name.
func (l *Local) remove(tenant uuid.UUID, id uuid.UUID) error {
	if tenant == uuid.Nil {
		return errors.New("file: remove needs the tenant the object lives under")
	}
	return l.erase(l.positions(tenant, id)...)
}

// erase removes every named position, a name with nothing at it already removed.
func (l *Local) erase(names ...string) error {
	for _, name := range names {
		if err := os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("file: remove %s: %w", name, err)
		}
	}
	return nil
}

// Blobs is contracts.Reconciler on the filesystem: every object written before
// before, each with the tenant whose directory it is in.
//
// It walks the store's own root and reads each entry's modification time, which
// is when the upload finished writing it. A directory that is neither a tenant id
// nor this app's own segment, and a file that is not a key, are skipped: they are
// not something this package wrote, and a sweep that deleted what it did not
// recognise would be a sweep that deletes a backup somebody left here — or, with
// two apps sharing one volume, another app's bytes.
//
// What is listed is therefore narrower than what is readable: an app that names
// itself lists its own segment, an app that names nothing lists the tenant
// directories, and both list the flat directory. tenantOf is that line, and a blob
// on the other side of it stays on the volume whatever its modification time says.
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

// tenantOf is whose object this is, from where it sits — and, for a store that
// names itself, whether these bytes are its own to remove at all.
//
// <app>/<tenant>/<key> is that tenant's, and only when the first segment is this
// store's own slug: two apps mounted at one root each see the other's directory
// and neither is the other's to sweep.
//
// <tenant>/<2>/<key> is that tenant's in the deployment that names none — and
// which of those tenants belong to an app that names itself is a row's answer,
// not a path's, so reconcile.go's foreignTenants drops the listed bytes of a
// tenant another app holds before any of them is removed.
//
// An app that names itself reads that position — Get, Delete and Prove must keep
// serving bytes written before its boot added the segment — but never lists it, and
// never removes what lives in it. The reason is one sentence: a listing is a list
// of things to delete, and nothing in that directory says which app wrote the
// bytes, so a named app that listed it would be claiming a directory it shares
// with every other app on the volume — including the deployment of one app whose
// live bytes are still being written there. Its own older blobs are in the same
// directory, and the command that closes that window (kit/appname/README.md,
// *Stored files*) is the `mv` into the app's own segment, after which this listing
// sees them and their age decides.
//
// The flat <dir>/<2>/<key> a release before the scope wrote is nobody's that this
// package can name, so it reports uuid.Nil and the sweep treats it as a blob no
// row claims; every app sharing the root can list it, and what protects a live
// blob in it is the row naming its key, which the sweep asks about across every
// tenant of the one database this server hosts (decision 0074).
func (l *Local) tenantOf(at string, key uuid.UUID) (uuid.UUID, bool) {
	rel, err := filepath.Rel(l.dir, at)
	if err != nil {
		return uuid.Nil, false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	switch len(parts) {
	case 3:
		if l.app.Named() {
			if parts[0] == l.app.String() && parts[2] == key.String() {
				return minted(parts[1])
			}
			return uuid.Nil, false
		}
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

// minted is a name that is a UUID and nothing else — which is the same test for
// a tenant's directory as for a key's file, because both are UUIDs this module
// minted and neither is anything a caller wrote.
func minted(name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(name)
	return id, err == nil && id != uuid.Nil
}
