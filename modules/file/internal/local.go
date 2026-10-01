package internal

import (
	"context"
	"errors"
	"fmt"
	"github.com/septagon-oss/platformkit/kit/appname"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"regexp"

	"github.com/septagon-oss/platformkit/kit/tenancy"
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
type Local struct {
	dir string

	// app is the slug whose name every stored object's path carries; with the
	// tenant it is what keeps two apps sharing one volume or one bucket from
	// writing one path. Empty is the deployment of one app, whose files sit where
	// they were written before this segment existed.
	app appname.Name
}

// NewLocal returns storage under dir. The directory is created when the first
// blob is written rather than here, so constructing this in a composition
// touches no disk.
func NewLocal(dir string) *Local { return NewLocalOf(appname.Name(""), dir) }

// NewLocalOf is NewLocal for a deployment that names its app: a stored object
// lands under <app>/<tenant>/<key> — the tenant is the one of the request that
// wrote it (see Local.tenant) — so two apps on one mounted volume cannot write
// one another's bytes and an operator can point a quota, a bucket policy or a
// `du` at one tenant's bytes. The persisted key is still the caller's UUID.
//
// Nothing reads the older `<dir>/<key[:2]>/<key>` position: an installation whose
// volume already holds bytes writes them nowhere new, it starts naming its app at
// a boot that moves them (`mv <dir>/<key[:2]>/<key> <dir>/<app>/<tenant>/<key>`, one
// move per blob, the row's tenant named in its own table), or it builds a second
// read path in the adapter. kit/appname/README.md's Stored-files row states the
// same thing as the window that is not open, which is the honest version of what
// an earlier draft of this comment claimed the code did.
func NewLocalOf(app appname.Name, dir string) *Local {
	return &Local{dir: dir, app: app}
}

var _ contracts.Storage = (*Local)(nil)

// key is a UUID, lower case, and nothing else.
var key = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// path is where the bytes for a key live, or an error for a key this package
// did not mint, or for an app that names itself with no tenant in the call.
func (l *Local) path(ctx context.Context, k string) (string, error) {
	if !key.MatchString(k) {
		return "", fmt.Errorf("file: %q is not a storage key; a key is a UUID", k)
	}
	id, err := uuid.Parse(k)
	if err != nil {
		return "", fmt.Errorf("file: %q is not a storage key; a key is a UUID", k)
	}
	tenant, err := l.tenant(ctx)
	if err != nil {
		return "", err
	}
	return l.join(appname.StoragePath(l.app, tenant, id)), nil
}

// join is the adapter's root with a name from kit/appname under it. One line
// spells that join, because the join of the root with a relative position is how
// a stored file's physical path is written, and the census counts the spelling
// (kit/appname/census_test.go): every position that reaches a path below comes
// from a constructor in that package, whichever of them the caller needed.
func (l *Local) join(rel string) string { return filepath.Join(l.dir, rel) }

// tenant is whose bytes this call is about.
//
// The persisted key is a UUID and names nothing, so the only thing that says
// whose bytes a blob is, is where they sit (appname.StoragePath) — and the row
// that holds the key belongs to the tenant whose request wrote it. The tenant is
// therefore read from the call: an upload and a download arrive in their
// request's context, and the removal subscription arrives in the transaction
// events.Consume scoped to the event's tenant. Refusing a call that names none
// is the point: writing an app's bytes under the nil UUID is one directory for
// every tenant of the app again, which is the layout the segment replaced while
// wearing the new name. With no slug set no path holds a tenant and the question
// does not arise — that deployment's layout is unchanged.
func (l *Local) tenant(ctx context.Context) (uuid.UUID, error) {
	if !l.app.Named() {
		return uuid.Nil, nil
	}
	if t, ok := tenancy.FromContext(ctx); ok && t.ID != uuid.Nil {
		return t.ID, nil
	}
	return uuid.Nil, fmt.Errorf("file: app %s stores under a tenant's directory and this call names none: the request or transaction that wrote the key says whose tenant the bytes belong to", l.app)
}

// locate is a Delete's path: the exact one when the call names the tenant, and
// otherwise the store's own walk for the key beneath this app's prefix.
//
// Exactly one caller arrives with no tenant: the reconciliation sweep. An orphan
// is by definition the blob no row references, so there is no row to ask whose
// tenant it was, and the sweep runs under system access because the question
// crosses every tenant by construction (see Reconcile). Searching is not a
// widening of scope: this package mints each key once, so at most one path under
// the app's own prefix holds it, and the walk never leaves that prefix. A Get
// never takes this branch — a read that cannot name a tenant has no business
// reading anybody's bytes.
func (l *Local) locate(ctx context.Context, k string) (string, error) {
	at, err := l.path(ctx, k)
	if err == nil {
		return at, nil
	}
	_, named := tenancy.FromContext(ctx)
	if !l.app.Named() || named || !key.MatchString(k) {
		// Either the layout names no tenant, which path answered above, or the
		// call did name one, or the key is not one this package minted: in every
		// one of those cases the walk would have nothing to do with the failure.
		return "", err
	}
	var found string
	root := l.join(appname.StorageRoot(l.app))
	walkErr := filepath.WalkDir(root, func(at string, e fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case !e.IsDir() && e.Name() == k:
			found = at
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.ErrNotExist) {
		return "", fmt.Errorf("file: look for %s under %s: %w", k, root, walkErr)
	}
	if found == "" {
		// Nothing at the key, which Delete answers as success rather than error.
		return "", nil
	}
	return found, nil
}

// Put writes the bytes, refusing a key that already exists: a key is minted per
// upload, so a collision is a bug rather than a replacement. size is ignored —
// a filesystem needs no length up front.
func (l *Local) Put(ctx context.Context, k string, r io.Reader, _ int64) error {
	at, err := l.path(ctx, k)
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
func (l *Local) Get(ctx context.Context, k string) (io.ReadCloser, error) {
	at, err := l.path(ctx, k)
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
// attempt succeeded would never stop. Which path holds them is locate's
// question: the tenant's own directory when the call names a tenant, the app's
// own tree when the caller is the sweep that cannot name one.
func (l *Local) Delete(ctx context.Context, k string) error {
	at, err := l.locate(ctx, k)
	if err != nil {
		return err
	}
	if at == "" {
		return nil
	}
	if err := os.Remove(at); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("file: remove %s: %w", at, err)
	}
	return nil
}
