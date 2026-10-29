// Package file is a mounted directory, which is how a Kubernetes Secret, a
// Vault-agent template and a Docker secret all arrive.
//
// The installation's own secret is `<dir>/<name>` and a tenant's is
// `<dir>/<tenant>/<name>` — one subdirectory per customer, which is the layout a
// projection writes and the one that makes the two address spaces impossible to
// collapse: a flat directory would answer one customer's secret to another
// customer's request, and that is the case the conformance suite exists for.
//
// Bytes are returned exactly as the file holds them, trailing newline included. A
// mounted value keeps the newline its producer wrote, and a secret whose newline is
// part of the secret is not this adapter's to guess about: the layer that speaks the
// protocol is the one that knows whether an SMTP password ends in CR-LF, so it trims
// there and not here.
package file

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/secrets"
)

// New returns the store over cfg's directory.
//
// It takes no context because a plan value is built in a module's wire, which has
// none, and because nothing here reaches a network: the check is that the directory
// is absolute and openable, so a composition that named a store it cannot read
// refuses at boot rather than failing the first request that needed a secret.
func New(cfg config.Secrets) (secrets.Store, error) {
	if cfg.Dir == "" {
		return nil, errors.New("secrets/file: secrets.dir is empty; the mounted directory is this adapter's whole answer")
	}
	if !filepath.IsAbs(cfg.Dir) {
		return nil, fmt.Errorf("secrets/file: secrets.dir %q is not an absolute path", cfg.Dir)
	}
	info, err := os.Stat(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("secrets/file: secrets.dir %s: %w", cfg.Dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("secrets/file: secrets.dir %s is not a directory", cfg.Dir)
	}
	return secrets.Trace("file", &store{dir: cfg.Dir}), nil
}

type store struct{ dir string }

func (s *store) Get(ctx context.Context, ref secrets.Ref) (secrets.Secret, error) {
	if err := ref.Check(ctx); err != nil {
		return secrets.Secret{}, err
	}
	path, err := s.path(ref)
	if err != nil {
		return secrets.Secret{}, err
	}
	info, err := s.stat(path)
	if err != nil {
		return secrets.Secret{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return secrets.Secret{}, fmt.Errorf("secrets: reading %s: %w", ref, err)
	}
	return secrets.Secret{Bytes: raw, Version: version(info)}, nil
}

func (s *store) Version(ctx context.Context, ref secrets.Ref) (string, error) {
	if err := ref.Check(ctx); err != nil {
		return "", err
	}
	path, err := s.path(ref)
	if err != nil {
		return "", err
	}
	info, err := s.stat(path)
	if err != nil {
		return "", err
	}
	return version(info), nil
}

func (s *store) Seal(ctx context.Context, ref secrets.Ref, plaintext []byte) ([]byte, error) {
	material, err := s.key(ctx, ref)
	if err != nil {
		return nil, err
	}
	return secrets.SealWith(ref, material, plaintext)
}

func (s *store) Open(ctx context.Context, ref secrets.Ref, sealed []byte) ([]byte, error) {
	material, err := s.key(ctx, ref)
	if err != nil {
		return nil, err
	}
	return secrets.OpenWith(ref, material, sealed)
}

// path is the address as a file: the installation's own name under the directory, a
// tenant's under its own subdirectory, named by the tenant id rather than a slug so
// two customers whose slugs differ by punctuation cannot collide.
func (s *store) path(ref secrets.Ref) (string, error) {
	if ref.Name() == "" {
		return "", secrets.ErrBadName
	}
	if ref.Tenant() == uuid.Nil {
		return filepath.Join(s.dir, ref.Name()), nil
	}
	return filepath.Join(s.dir, ref.Tenant().String(), ref.Name()), nil
}

// stat is the one door a read passes, and it answers ErrNotFound rather than a
// permission or a traversal sentence for everything it refuses.
//
// The refusal of a symlink is the subtle half: a mounted Secret volume *is*
// symlinks — every data file points through `..data` — so refusing links would
// refuse the deployment this package exists for. What is refused is where a link
// lands: the resolved path has to stay inside this store's own directory, which
// is the property that matters, because a name that escaped it is the one thing
// an operator did not mount. kit/secrets' grammar already refuses a name with a
// slash or a leading dot, so the only escape left is one somebody wrote into the
// directory, and this adapter refuses to walk out of it.
//
// A directory, a socket, a device and a dangling link are all misses: none of
// them is a secret this deployment mounted.
func (s *store) stat(path string) (os.FileInfo, error) {
	// The store itself first: a directory that is not there is not a name that is
	// not there, and an unmounted volume whose every secret read answered
	// ErrNotFound would be a deployment that sent mail with no password.
	root, err := filepath.EvalSymlinks(s.dir)
	if err != nil {
		return nil, fmt.Errorf("secrets: secrets.dir %s is not readable: %w", s.dir, err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("secrets: reading %s: %w", base(path), err)
	}
	info, err := os.Stat(resolved)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("secrets: reading %s: %w", base(path), secrets.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("secrets: reading %s: %w", base(path), err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("secrets: reading %s: %w", base(path), secrets.ErrNotFound)
	}
	inside, err := filepath.Rel(root, filepath.Dir(resolved))
	if err != nil || filepath.IsAbs(inside) || inside == ".." ||
		strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("secrets: reading %s: %w", base(path), secrets.ErrNotFound)
	}
	return info, nil
}

// key reads the key material under the same address a secret uses: one key per
// name, so rotating it means re-sealing everything written under the old one —
// which is the asymmetry a key store (OpenBao's transit) does not have, and why
// rotation is each adapter's own assertion rather than the suite's.
func (s *store) key(ctx context.Context, ref secrets.Ref) ([]byte, error) {
	if err := ref.Check(ctx); err != nil {
		return nil, err
	}
	path, err := s.path(ref)
	if err != nil {
		return nil, err
	}
	if _, err := s.stat(path); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("secrets: reading the key at %s: %w", base(path), err)
	}
	return raw, nil
}

// version is the store's token for one file's bytes: its modification time, in UTC.
//
// Not a digest of the contents, because a digest of a low-entropy secret answers a
// guess offline, and not a size, because two passwords can be the same length. The
// mtime is what a rotation through a `..data` swap moves — the file is replaced, and
// the new link's target carries a new time — which is the fact a caller with a real
// invalidation path would cache on if this port ever had one.
func version(info os.FileInfo) string {
	return info.ModTime().UTC().Format(time.RFC3339Nano)
}

func base(path string) string { return filepath.Base(path) }
