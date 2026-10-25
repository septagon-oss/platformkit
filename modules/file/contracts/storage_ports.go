package contracts

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// Storage is where the bytes go. There are two implementations in this module:
// one on local disk, which is what a laptop and a mounted volume are, and one
// that speaks S3 to any object store. The interface is what makes the choice a
// wiring decision rather than a rewrite.
//
// Every call names a Scope. That is the change from a port that took a bare
// key: a bucket shared by every tenant of a shared-instance deployment (see
// decision 0028) cannot keep one tenant out of another's bytes by hoping the
// key was well-formed, so an adapter derives each object's prefix from the
// scope and a key has no way to name a prefix that is not its own tenant's.
//
// It takes no transaction, and that is still the shape of the whole module: a
// blob write cannot be rolled back, so it happens outside one on purpose and
// the order relative to the commit is chosen at each call site. A Scope is not
// a transaction — it is scope and nothing else, which is why an upload can
// stream a body for as long as the client likes without holding a connection.
type Storage interface {
	// Put writes the bytes at key under scope, with meta beside them. size is
	// what the caller declared, or -1 when nothing did, and every implementation
	// has to write the bytes it was handed either way: this module's own door
	// streams the request and so declares no length at all
	// (internal/handler.go's arriving), which means an implementation that
	// refused -1 would be refusing the upload route. The one on disk ignores the
	// number outright. What a store may do with a length it was given is answer
	// the request as one write rather than several.
	//
	// Writing a key that already exists is an error whatever the length was,
	// because a key is minted per upload and a collision is a bug rather than a
	// replacement — including over a stream that declared none, which an object
	// store cannot cover with its own conditional create header and so has to
	// claim before it fills (internal/s3.go).
	Put(ctx context.Context, scope Scope, key Key, r io.Reader, size int64, meta Meta) error

	// Get opens the bytes at key, or ErrNoBlob when there are none. The caller
	// closes what it is given.
	Get(ctx context.Context, scope Scope, key Key) (io.ReadCloser, error)

	// Delete removes the bytes at key. A key with nothing at it is not an
	// error: the worker that calls this retries, and a retry that failed
	// because the first attempt succeeded would never stop.
	Delete(ctx context.Context, scope Scope, key Key) error
}

// Reconciler is the half of Storage a sweep can be built on, and it is
// optional: an implementation that cannot enumerate what it holds — a
// signed-URL gateway, a store behind somebody else's API — simply does not
// implement it, and the module mounts no sweep over a store it cannot count.
//
// It exists because an orphan blob is the one inconsistency this module's own
// ordering produces on purpose: an upload writes the bytes before the row, so a
// transaction that then fails leaves bytes nobody references. Nothing in the
// database records them, which is why the sweep has to start from the store.
//
// It replaces Lister, and the transaction in its signature is why: enumerating
// a shared bucket is an installation-wide read by construction, so it is system
// scoped in the type rather than a call that happens to run from a job. A
// tenant transaction cannot ask this question, which is the difference between
// a port that can be handed to a request by mistake and one that cannot.
type Reconciler interface {
	// Blobs is every object written before before. The bound is the whole
	// safety argument: an upload in flight has bytes and no row yet, and a
	// sweep that did not exclude it would delete the blob out from under a
	// request that is about to commit.
	//
	// Each Blob is located by the store that holds it. TenantID is uuid.Nil
	// where the store cannot know — Local's flat directory records no tenant —
	// and the object's own tenant wherever the store keeps them apart.
	Blobs(ctx context.Context, tx db.Tx[db.System], before time.Time) ([]Blob, error)

	// RemoveBlob deletes one object the listing returned. Removing what is
	// already gone is success and not an error, as Delete promises.
	RemoveBlob(ctx context.Context, tx db.Tx[db.System], b Blob) error
}

// Blob is one object, located by the store that holds it.
type Blob struct {
	// TenantID is uuid.Nil where the store cannot know whose it is.
	TenantID uuid.UUID
	Key      Key
}

// Prover is the second question a Reconciler answers, and the one an erasure
// needs: after the delete, does anything still answer to that name? It counts
// objects, versions, delete markers and abandoned multipart parts, because a
// store that keeps any of them is a store where "the bytes are gone" needs the
// listing to mean it.
//
// It is optional like Reconciler and Signer. A store that cannot answer is not
// refused its erasure — the proof row is still written, and its verified_at
// stays NULL, which is the difference between "we checked" and "we assume"
// rather than something a caller gets to talk this module out of.
type Prover interface {
	Prove(ctx context.Context, scope Scope, key Key) (seen int, err error)
}

// Signer is the door that hands out a time-limited read onto one object with no
// request of this process in the path, and like Reconciler it is optional: a
// store that cannot do it — the one on local disk — does not implement it, and
// the grant command answers ErrNotSignable rather than pretending.
type Signer interface {
	// Sign returns a URL that reads f's bytes until expiry. It is the store's
	// own presigning (an S3 SigV4 query, for the store here) and not a token
	// this module invented, so the thing that has to be correct is a signature
	// standard and not a session of our own devising. f carries the visibility
	// and the type, which is what the object's metadata already says, so a
	// caller cannot ask for a longer cache or a friendlier disposition than the
	// row implies.
	Sign(ctx context.Context, scope Scope, f *File, expiry time.Duration) (*Grant, error)
}

// Grant is a door with an expiry on it. It is a value and never a row: minting
// one creates nothing, there is nothing to revoke, and it is gone when it is
// expired. It is a bearer capability until ExpiresAt, which is why the expiry is
// short, why the mint is logged, and why removing the bytes revokes it.
type Grant struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt" format:"date-time"`
}

// Upload is one arriving file: what the caller said about it, and the bytes.
// The reader is streamed straight to Storage while it is hashed and counted, so
// nothing here is ever held in memory or spooled to a temporary file.
type Upload struct {
	Name        string
	ContentType string
	Visibility  string
	// Kind is what the product calls this file — an opaque token like
	// "invoice" or "chat.photo", matched against config.Files.Retention by the
	// retention sweep. This module never branches on a value of it and names
	// none: the class is the product's, the duration it means is the
	// deployment's, and the only thing the module knows is that a kind with no
	// configured policy is never deleted.
	Kind string
	// Sealed stores the bytes with no door of this module: the file module's content
	// routes refuse the row and mint no presigned URL for it, so the only thing that
	// can answer its bytes is the caller that stored them, through its own door and
	// its own authorisation. A module that seals what it stores takes responsibility
	// for handing it back, and an upload that seals and never serves is a file
	// nobody can read — which is why nothing here infers the flag from a name or a
	// media type, and only the storing module says it.
	Sealed bool
	// Declared is the length the request declared, or -1, which is what a
	// streamed part honestly declares. It is a hint a Storage may use to ask
	// the store for one write instead of several, and nothing else: the size
	// that is stored and the limit that is enforced both come from counting the
	// bytes as they go past, because a request that declares a length can be
	// wrong about it either by accident or on purpose. No implementation may
	// refuse -1, because this module never declares anything else here.
	Declared int64
	Body     io.Reader

	// Image is the caller saying "this is an image, treat it as one": set by
	// the media library and by the editor's image field. It costs nothing when
	// it is true and it changes what a refusal is: an upload the caller called
	// an image is refused with ErrNotImage when no decoder reads it, where the
	// same bytes offered as a document stay the attachment they always were
	// (decision 0069 §4). It asks for no permission the upload does not already
	// have, and it is never a promise about what the bytes are.
	Image bool
}

// Opener is the one thing a consuming module actually needs: the bytes of a
// file it knows the id of. A module that renders a page with an image, or a
// client module that hands a stored document to a printer, takes this and not
// Service — it cannot upload, cannot delete, and reads the same rows its own
// transaction can see.
//
// It is separate from Service for the reason every narrow interface here is:
// what a consumer declares is what a reviewer reads as the dependency, and
// "this module can delete files" is a different sentence from "this module can
// read one".
type Opener interface {
	// Open is the row and its bytes. anonymous is the public door: it serves a
	// file when somebody may read it — because its own visibility says so, or
	// because a record with a public face is showing it — and answers ErrNotFound
	// for everything else, so a private file and a file that does not exist are the
	// same answer to a caller who is not signed in. The row handed back carries the
	// answer the door gave, so what is served, the cache policy written on it and
	// what the caller goes on to report about the file are one verdict.
	Open(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, anonymous bool) (*File, io.ReadCloser, error)
}

// Tx opens the caller's transaction. Upload takes one of these rather than an
// open transaction, and it is the only command here that does.
//
// A body arrives at whatever speed the client chooses to send it, so a
// transaction opened before the first byte is a database connection one slow
// client pins for the whole of the server's read timeout — a review trickled an
// upload at a byte a second and watched it hold one. The bytes are therefore
// streamed with nothing open, and this is called once they are all in storage.
// kit/httpx's per-request transaction is already lazy, so the route hands over
// its accessor and nothing is opened until this module asks for it.
type Tx func(ctx context.Context) (db.Tx[db.Tenant], error)
