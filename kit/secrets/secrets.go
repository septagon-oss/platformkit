// Package secrets is the door to a deployment's secrets and to its keys: what
// are this name's bytes and which version of them, and wrap this plaintext
// under that key and give it back. Every cloud keeps these bytes somewhere
// else, and every module that needed one had read its own environment variable
// for it, which left a deployment with no plug to point at and six places that
// each believed they knew who read which secret.
//
// The address is either a tenant's or the installation's, no caller can form
// one without saying which, and a read whose context resolved another tenant is
// refused before anything is looked up. What a read writes is the name, the
// adapter and a length, never the bytes, so a trace collector cannot become a
// secret store. The port stores, caches, emits and instruments nothing, and
// README.md reasons about each absence.
package secrets

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

var (
	// ErrBadName is what every method answers for a Ref that Of or Shared never
	// built, or a name the grammar refuses: a path, a hidden file, a folded
	// spelling, an empty name, a name too long to be a file's. It is immutable — a
	// caller bug, and no bytes are read to answer it.
	ErrBadName = errors.New("secrets: this name is not one this port can address")

	// ErrNotFound is what every method answers when the store is answering and has
	// no such name. It is correctable: the operator mounts the secret and the next
	// call succeeds. It is never an outage and never a refusal — a caller that
	// treats it as a refusal 404s a secret that was simply never mounted, and a
	// caller that treats an outage as it loses a credential silently.
	ErrNotFound = errors.New("secrets: this store holds no secret under this name")

	// ErrNoScope is what a read of a tenant's Ref answers when the context resolved
	// a different tenant, or resolved none. It is immutable: a cross-tenant read is
	// a caller bug, and it is refused rather than logged.
	ErrNoScope = errors.New("secrets: this context does not hold the tenant this secret belongs to")

	// ErrSealed is what Seal answers for empty plaintext and Open answers for bytes
	// this port did not write: a truncated seal, a seal made under another key or
	// for another address, a ciphertext from a foreign key store, and key material
	// that is not one of this port's keys. All of them are defects rather than
	// conditions, and none of them is answered with the plaintext.
	ErrSealed = errors.New("secrets: these bytes are not this port's envelope under this key")
)

// nameBytes bounds a name. It is the length a single file name and a single
// environment variable may both carry, and the ceiling of a KV path segment.
const nameBytes = 63

// Ref is one secret's, or one key's, address inside a store.
//
// Its fields are unexported because the half of an address that decides who may
// read it is *which constructor it went through*: Of, for a secret belonging to a
// customer, or Shared, for one belonging to the installation. A Ref assembled by
// struct literal means nothing and is refused by every method, exactly as a
// cache.Scope assembled by struct literal is.
type Ref struct {
	tenant uuid.UUID
	name   string
}

// Of is the address of a secret, or a key, that belongs to a tenant.
//
// The tenant is a value and not a string, and it arrives from tenancy.FromContext
// or a db.Tx[db.Tenant] — the places this kernel knows a tenant because something
// checked. A name the grammar refuses, or the nil UUID, returns the zero Ref: a
// caller that resolved nothing has no customer's secret to ask for, and Shared is
// the door for the installation's own.
func Of(tenant uuid.UUID, name string) Ref {
	if tenant == uuid.Nil || badName(name) {
		return Ref{}
	}
	return Ref{tenant: tenant, name: name}
}

// Shared is the address of a secret, or a key, that belongs to the installation
// and to no tenant: the deployment's own SMTP password, the relay credential, the
// signing key. It is called Shared rather than being Of(nil) so that every site
// holding a customer's secret is a site that names a tenant.
func Shared(name string) Ref {
	if badName(name) {
		return Ref{}
	}
	return Ref{name: name}
}

// Name is this address's own name, or "" for an address that names nothing.
func (r Ref) Name() string { return r.name }

// Tenant is the tenant that owns this address, or uuid.Nil for the installation's
// own. It answers the ownership; it does not establish that the caller holding the
// address may read it, which is what Check is for.
func (r Ref) Tenant() uuid.UUID { return r.tenant }

// String is the address as it may reach a log line, a span or an error: the name,
// and the tenant's first eight hex digits for a tenant's address. Never a value,
// and never a full tenant id, so an address quoted beside a request id cannot
// become a way to enumerate one customer's secrets from a log.
func (r Ref) String() string {
	switch {
	case r.name == "":
		return "(no name)"
	case r.tenant == uuid.Nil:
		return "shared/" + r.name
	default:
		return "tenant/" + r.tenant.String()[:8] + "/" + r.name
	}
}

// AAD is the bytes a local seal binds itself to: the whole address, tenant included.
//
// The address is in the seal rather than beside it so a ciphertext written for one
// name, or one tenant, cannot be opened under another. The tenant arrives as its 16
// bytes and the name follows, so no escaping of the name's own separators is
// needed — this is the AAD, not a spelling anybody parses.
func (r Ref) AAD() []byte {
	if r.name == "" {
		return nil
	}
	head := make([]byte, 0, len(uuid.UUID{})+len(r.name))
	if r.tenant != uuid.Nil {
		head = append(head, r.tenant[:]...)
	}
	return append(head, r.name...)
}

// Check refuses a read this context may not make, and is the whole of this port's
// authorization answer. The port grants nothing and checks no permission: whether
// a caller may read a name at all is the caller's own command's grant, inside its
// transaction. What is decided here is only whose secret the address claims.
//
// A Shared address is open to any caller that reaches the port. A tenant's address
// needs a context that resolved that tenant — the same rule row-level security
// enforces with a session setting, run at a door that opens no row. A mismatch is
// ErrNoScope, and a zero Ref is ErrBadName.
func (r Ref) Check(ctx context.Context) error {
	if r.name == "" {
		return ErrBadName
	}
	if r.tenant == uuid.Nil {
		return nil
	}
	resolved, ok := tenancy.FromContext(ctx)
	if !ok || resolved.ID != r.tenant {
		return fmt.Errorf("secrets: reading %s: %w", r, ErrNoScope)
	}
	return nil
}

// Secret is one answer: opaque bytes, and the store's own token for which bytes
// they are.
type Secret struct {
	Bytes   []byte
	Version string
}

// Secrets is the read half of the port.
type Secrets interface {
	// Get answers the bytes under ref. An absent name is ErrNotFound, and an
	// unreachable store is an error that is not ErrNotFound: a miss and an outage
	// are never the same answer, because a caller that cannot tell them apart
	// either fails open over a dead store or loses a credential silently.
	//
	// The bytes are exactly what the store holds, including a trailing newline a
	// mounted secret arrived with: the layer that speaks the protocol decides what
	// to do about it, and an adapter that trimmed would be corrupting a secret
	// whose newline is part of the secret.
	Get(ctx context.Context, ref Ref) (Secret, error)

	// Version answers the store's token for the bytes Get would answer now, without
	// answering them. It moves when and only when those bytes move, and it is
	// comparable and opaque: nobody parses it, and nobody derives anything from it.
	// "" is a store that has no token at all — an environment variable has none, and
	// inventing one would be either a digest of a low-entropy secret or a number
	// that moves when nothing did.
	Version(ctx context.Context, ref Ref) (string, error)
}

// Keys is the envelope half: this deployment's key store, which is not the same
// thing as its secret store even when they are the same server.
type Keys interface {
	// Seal wraps plaintext under the named key into bytes that are safe to store in
	// a column. Empty plaintext is refused with ErrSealed; a nonce is drawn per
	// call, so two seals of one plaintext are different bytes and both open. The
	// address is bound into the seal as additional authenticated data, so these
	// bytes cannot be replayed under another name or another tenant.
	Seal(ctx context.Context, ref Ref, plaintext []byte) ([]byte, error)

	// Open is the other half, under the same ref. Bytes this port did not write —
	// truncated, foreign, sealed for another address or under another key — are
	// ErrSealed, and the answer never carries a guess at the plaintext.
	Open(ctx context.Context, ref Ref, sealed []byte) ([]byte, error)
}

// Store is what one adapter implements and what the conformance suite takes: both
// halves, over the same address space, so a deployment that mounts a directory
// reads its secrets and seals with the keys in that same directory.
type Store interface {
	Secrets
	Keys
}

// badName is the shape test of a name: 1 to 63 bytes of lower-case letters,
// digits, dot, underscore and hyphen, starting and ending alphanumeric.
//
// It is the subset all three stores can spell — a file name, a URL path segment
// and an environment variable's tail — and it is the one place path traversal is
// refused, so an adapter that joins a name into a path has nothing to escape from.
// A dot is legal so a secret may be named after the configuration key it replaces
// (mail.password), which is what makes a move invisible to an operator. Case is
// refused rather than folded: two operators spelling one secret two ways is the
// bug folding would hide.
func badName(name string) bool {
	if len(name) == 0 || len(name) > nameBytes {
		return true
	}
	if !alphanumeric(name[0]) || !alphanumeric(name[len(name)-1]) {
		return true
	}
	for i := 1; i < len(name)-1; i++ {
		if b := name[i]; !alphanumeric(b) && b != '.' && b != '_' && b != '-' {
			return true
		}
	}
	return false
}

func alphanumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}
