// Package env is the secret store every deployment already has: its own process
// environment.
//
// It is the adapter a laptop and a one-container deployment run on, and it opens
// nothing — no file handle, no socket, no credential to bootstrap — which is why
// an unset secrets.adapter means this one. It is also the adapter that reaches the
// operator's existing habit rather than replacing it: PLATFORMKIT_MAIL_PASSWORD is
// both the variable kit/config read until this package existed and the name a
// Shared("mail.password") maps to, so a deployment that moves onto the port does
// not change its environment.
//
// Two limits follow from the shape of an environment and are stated rather than
// worked around. There is no version: an environment variable has no token for
// which bytes it holds, and inventing one would be either a digest of a
// low-entropy secret or a number that moves when nothing relevant did. And there
// are no tenants: a flat process environment cannot answer a different value per
// customer, so a tenant's address is refused with ErrNotSupported rather than
// answered with somebody else's secret. The fix belongs to the deployment — mount
// file, or set secrets.adapter openbao — and the conformance suite's tenant cases
// run against the adapters that answer them.
package env

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/secrets"
)

// ErrNotSupported is this adapter's refusal of a tenant's address: the process
// environment is one flat set of names for one process, so it cannot hold a
// different value per customer. It is immutable — the answer is a different
// adapter, not a retry and not a mount of a variable that cannot mean it.
var ErrNotSupported = errors.New("secrets/env: a tenant's secret needs the file or openbao adapter")

// prefix marks every name this port reads, so a secret the port resolves is
// distinguishable in a process's environment from the hundred variables a
// container runtime puts there.
const prefix = "PLATFORMKIT_"

// Source is where this adapter reads one variable: it answers the value, or
// ErrNotFound for a name that is not set and an error that is not ErrNotFound for
// a lookup that could not be made.
//
// It is a function rather than os.LookupEnv called inline because the distinction
// an adapter must not get wrong is between "unset" and "unreadable", and a test of
// that distinction needs to be able to produce the second one.
type Source func(name string) (string, error)

// New returns the store over src.
func New(src Source) (secrets.Store, error) {
	if src == nil {
		return nil, errors.New("secrets/env: New needs a source to read")
	}
	return secrets.Trace("env", &store{src: src}), nil
}

// FromOS is New over the process's own environment.
//
// An unset variable answers ErrNotFound and never the pair ("", nil): a store that
// answers an empty value for a name nobody set is a store that turns a missing
// credential into an empty password, and the relay's 535 is a long way from here.
func FromOS() (secrets.Store, error) {
	return New(func(name string) (string, error) {
		value, ok := os.LookupEnv(name)
		if !ok {
			return "", secrets.ErrNotFound
		}
		return value, nil
	})
}

type store struct{ src Source }

func (s *store) Get(ctx context.Context, ref secrets.Ref) (secrets.Secret, error) {
	raw, err := s.read(ctx, ref)
	if err != nil {
		return secrets.Secret{}, err
	}
	// No version, on purpose: see the package's second limit. An environment
	// variable's bytes are whatever the process was started with.
	return secrets.Secret{Bytes: []byte(raw)}, nil
}

func (s *store) Version(ctx context.Context, ref secrets.Ref) (string, error) {
	if _, err := s.read(ctx, ref); err != nil {
		return "", err
	}
	return "", nil
}

func (s *store) Seal(ctx context.Context, ref secrets.Ref, plaintext []byte) ([]byte, error) {
	material, err := s.read(ctx, ref)
	if err != nil {
		return nil, err
	}
	return secrets.SealWith(ref, material, plaintext)
}

func (s *store) Open(ctx context.Context, ref secrets.Ref, sealed []byte) ([]byte, error) {
	material, err := s.read(ctx, ref)
	if err != nil {
		return nil, err
	}
	return secrets.OpenWith(ref, material, sealed)
}

// read resolves one address into the variable's bytes, after the scope check the
// port owes every read.
func (s *store) read(ctx context.Context, ref secrets.Ref) ([]byte, error) {
	if err := ref.Check(ctx); err != nil {
		return nil, err
	}
	name, err := variable(ref)
	if err != nil {
		return nil, err
	}
	raw, err := s.src(name)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return nil, fmt.Errorf("secrets: reading %s from %s: %w", ref, name, secrets.ErrNotFound)
		}
		// An outage keeps its own shape: the caller that fails open on a miss must
		// not fail open on a store that stopped answering, and it cannot tell them
		// apart if this wraps one as the other.
		return nil, fmt.Errorf("secrets: reading %s from %s: %w", ref, name, err)
	}
	return []byte(raw), nil
}

// variable is the address as an environment name: PLATFORMKIT_MAIL_PASSWORD for
// Shared("mail.password"). Upper-casing and the dot and hyphen folding are the
// mapping every container runtime already makes of a dotted key, and the name
// grammar has already refused everything else that could not be spelled.
//
// A tenant's address has no spelling here, which is ErrNotSupported rather than a
// guess that folds the tenant id into the name: an environment that carries one
// customer's password per suffix is an environment nobody can audit.
func variable(ref secrets.Ref) (string, error) {
	if ref.Tenant() != uuid.Nil {
		return "", fmt.Errorf("secrets/env: reading %s: %w", ref, ErrNotSupported)
	}
	var builder strings.Builder
	builder.WriteString(prefix)
	for i := 0; i < len(ref.Name()); i++ {
		switch b := ref.Name()[i]; b {
		case '.', '-':
			builder.WriteByte('_')
		default:
			builder.WriteByte(upper(b))
		}
	}
	return builder.String(), nil
}

func upper(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - ('a' - 'A')
	}
	return b
}
