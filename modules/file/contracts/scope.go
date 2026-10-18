package contracts

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Scope is the tenant a byte belongs to.
//
// The port used to take a bare key, which made containment a habit an adapter
// was expected to keep: Local derived the whole path from the key, so the only
// thing standing between a caller and another tenant's bytes was the UUID
// regex, and nothing at all stood between them once the bytes moved into one
// bucket shared by every tenant of a shared-instance deployment (decision 0028).
// So the scope is in the type now, and an adapter derives the prefix of every
// object name from it rather than from anything a caller wrote.
//
// Its one field is unexported and there are two doors that mint one: ScopeOf,
// which reads the tenant the HTTP layer resolved for this request, and
// ScopeOfTx, whose argument is a db.Tx[db.Tenant], so a transaction opened
// against the whole installation does not compile here. A zero Scope carries
// uuid.Nil and every implementation refuses it.
//
// Two honest limits, because a reviewer will look for both. (a) tenancy.Tenant
// is exported, so a caller could forge a context and mint a Scope for a tenant
// it has no business serving — but that caller can already open every row of
// that tenant, because db.Run reads the same context and row-level security
// follows it. The scope is exactly as strong as the kernel's own tenancy, which
// is the database's. (b) It is stronger in one respect: an adapter handed a
// Scope receives no *gorm.DB at all, so it cannot query the database to check
// what a caller claimed.
type Scope struct{ tenant uuid.UUID }

// ScopeOf is the door a request uses: the tenant this context resolved.
func ScopeOf(ctx context.Context) (Scope, error) {
	t, ok := tenancy.FromContext(ctx)
	if !ok || t.ID == uuid.Nil {
		return Scope{}, db.ErrNoTenant
	}
	return Scope{tenant: t.ID}, nil
}

// ScopeOfTx is the door a caller holding a transaction uses. The scope comes
// from the handle's type, which is what makes a db.Tx[db.System] impossible
// here rather than merely discouraged.
func ScopeOfTx(tx db.Tx[db.Tenant]) Scope {
	return Scope{tenant: db.TenantOf(tx).ID}
}

// TenantID is the tenant this scope names, uuid.Nil for the zero value.
func (s Scope) TenantID() uuid.UUID { return s.tenant }

// String is what an implementation puts in front of a key: the tenant's own
// prefix, which no caller composes and no key can name.
func (s Scope) String() string { return s.tenant.String() }

// Key is the name Storage is asked for: a minted UUID and nothing else.
//
// It is a defined string type, and a defined string type cannot refuse
// Key("../../etc/passwd") at compile time — only the parse can. Every
// implementation therefore re-checks the shape on the way in, exactly as
// internal.Local has always done, and filetest.RunStorage feeds the malformed
// literal to every one of them on purpose.
type Key string

// keyShape is a lower-case UUID and nothing else: no path separator, no dot-dot,
// no scheme, no percent-encoding, no upper case. It is the whole path-traversal
// argument, written once and re-used by every adapter.
var keyShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ParseKey refuses anything this module could not have minted.
func ParseKey(raw string) (Key, error) {
	if !keyShape.MatchString(raw) {
		return "", fmt.Errorf("%w: %q is not a storage key; a key is a lower-case UUID", ErrInvalidKey, raw)
	}
	return Key(raw), nil
}

// String lets an adapter write a Key into an object name without casting.
func (k Key) String() string { return string(k) }

// ObjectName is the full name of this key inside a tenant's prefix. It is the
// one place a prefix and a key are joined, so that the composition an
// implementation is asked to honour is the one a test can read.
func (s Scope) ObjectName(k Key) (string, error) {
	if s.tenant == uuid.Nil {
		return "", fmt.Errorf("%w: %s has no tenant to live under", db.ErrNoTenant, k)
	}
	if _, err := ParseKey(k.String()); err != nil {
		return "", err
	}
	return s.tenant.String() + "/" + k.String(), nil
}

// Meta is what a store is asked to keep beside the bytes.
//
// It exists because a presigned response has no handler in this process to set
// headers: whatever Content-Type, Cache-Control and Content-Disposition the
// object carries is what the client that reads it straight off the bucket is
// served. So the module hands the store the same answers contracts/response.go
// writes on a response it serves itself, and the adapter that can carry them
// writes them at Put. A store that cannot — Local — ignores the whole struct,
// and the difference is stated rather than discovered: everything served
// through this process gets the headers from response.go either way.
type Meta struct {
	// ContentType is the type the upload declared, which Validate has already
	// refused to disagree with the bytes for anything renderable.
	ContentType string
	// CacheControl is the word from docs/cache.md that matches this file's
	// visibility: no-store for a private file, immutable for a public one whose
	// address is a minted UUID that can never be re-pointed.
	CacheControl string
	// ContentDisposition is attachment unless the type is Renderable, which is
	// the rule contracts/disposition follows for a served response.
	ContentDisposition string
}

// ErrInvalidKey is a key Storage was asked for that this module could not have
// minted: not a UUID, or carrying a path, a dot-dot or a scheme. A key is never
// caller-supplied, so nothing that arrives here came from a request that could
// be corrected — it is a programming error, and it is refused before a single
// byte is read.
var ErrInvalidKey = errors.New("file: not a storage key")

// ImmutableLifetime is the Cache-Control a public object carries. Its address is
// a minted UUID that can never be re-pointed — Put refuses an existing key and
// Delete removes the row outright — which is the same condition docs/cache.md
// requires of a kernel asset at its content-naming address. The page that points
// at the bytes keeps its own shorter policy from the kernel; only the bytes are
// immutable.
const ImmutableLifetime = "public, max-age=31536000, immutable"

// MetaFor is the metadata an object carries, from the row that names it. It is
// one function rather than a rule each adapter guesses, because the answer has
// to be the one response.go gives for the same file.
func MetaFor(f *File) Meta {
	m := Meta{ContentType: f.ContentType, ContentDisposition: disposition(f, Attachment)}
	if f.Public() {
		m.CacheControl = ImmutableLifetime
	} else {
		m.CacheControl = downloadPrivate
	}
	return m
}
