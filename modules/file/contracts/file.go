// Package contracts is everything another module, an app or a test may know
// about files: the entity, the events, the permissions, the storage this module
// needs somebody else to satisfy, and the Service interface. The
// implementation is in ../internal.
//
// A file is a row and a blob, and the two live in different places: the row is
// in Postgres, under the tenant's own policy, and the bytes are wherever
// Storage puts them. Everything difficult about this module comes from that
// split — a blob write cannot be rolled back, and a row that survives a rollback
// its blob did not is a download that 500s — so where each of the two happens
// relative to the transaction is written down at every step.
package contracts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The two visibilities. A private file is served to a caller holding file:read;
// a public one is served to anybody at the public route, which is what an image
// in a published page has to be.
const (
	VisibilityPrivate = "private"
	VisibilityPublic  = "public"
)

// MaxName is the longest file name kept. A name is a label a person reads, and
// the bytes are found by a uuid, so nothing depends on it being long.
const MaxName = 255

// ErrTooLarge is an upload past the limit the deployment set. It is a failure
// of its own rather than one of kit/crud's three, because the answer is 413 and
// nothing else in the application has one.
var ErrTooLarge = errors.New("file: larger than this deployment accepts")

// ErrNoBlob is a key Storage has nothing at. It is what a Get answers with when
// the row says there are bytes and there are not, which is the one inconsistency
// the split between a row and a blob can produce.
var ErrNoBlob = errors.New("file: no bytes at this key")

// File is one uploaded blob's row.
//
// Every field but Name and Visibility is the server's account of what arrived:
// a caller does not get to say how big their upload was or what its digest is,
// because those are the two things a reader checks it against.
type File struct {
	crud.Base

	// Name is what the browser called it, kept for a person to read and used
	// for nothing else.
	Name string `json:"name" gorm:"type:varchar(255);not null" validate:"required" minLength:"1" maxLength:"255" doc:"The name the file was uploaded under" example:"invoice.pdf"`
	// ContentType is what the upload declared, and it is what the download
	// answers with. It is not sniffed: a reference architecture that guessed
	// would be teaching a guess, and the download is served with
	// X-Content-Type-Options: nosniff so a browser does not guess either.
	ContentType string `json:"contentType" gorm:"type:varchar(120);not null;default:'application/octet-stream'" maxLength:"120" doc:"Media type, as the upload declared it" example:"application/pdf" required:"false"`
	// Size and SHA256 are what actually arrived, counted and hashed in the one
	// pass that wrote the bytes.
	Size   int64  `json:"size" gorm:"not null" doc:"Bytes" readOnly:"true" required:"false"`
	SHA256 string `json:"sha256" gorm:"type:char(64);not null" doc:"SHA-256 of the content, hex" readOnly:"true" required:"false"`

	// StorageKey is where the bytes are. It is a UUID and nothing else, which
	// is the whole of the path-traversal argument: there is no caller-supplied
	// component in it to escape with.
	StorageKey string `json:"-" gorm:"type:varchar(64);not null"`

	// Visibility decides which door serves it. It defaults to private, and it
	// is spelled as a closed set rather than a bool so that the third answer
	// somebody will want — signed URLs — is a value and not a schema change.
	Visibility string `json:"visibility" gorm:"type:varchar(10);not null;default:'private'" enum:"private,public" ui:"widget:select" doc:"Who may read it" default:"private" required:"false"`

	// UploaderID is whoever uploaded it, a user id with no foreign key behind
	// it. Validate stamps it from the caller on the context, the way content
	// stamps an author.
	UploaderID uuid.UUID `json:"uploader,omitempty" gorm:"column:uploader_id;type:uuid" format:"uuid" ui:"hide:list" doc:"The user who uploaded it" readOnly:"true"`
}

// TableName pins the table, so the entity and migrations/000019 agree.
func (File) TableName() string { return "files" }

// Public reports whether anybody may read this file.
func (f *File) Public() bool { return f.Visibility == VisibilityPublic }

// Validate is the entity's own check, run by kit/crud on every write whichever
// door it came through.
func (f *File) Validate(ctx context.Context) error {
	f.Name = strings.TrimSpace(f.Name)
	f.ContentType = strings.TrimSpace(f.ContentType)
	if f.ContentType == "" {
		f.ContentType = "application/octet-stream"
	}
	if f.Visibility == "" {
		f.Visibility = VisibilityPrivate
	}
	if f.UploaderID == uuid.Nil {
		// The context is the one thing that knows whose request this is, which
		// is the same reason kit/events reads the actor off it.
		if actor, ok := tenancy.ActorFrom(ctx); ok {
			f.UploaderID = actor
		}
	}
	switch {
	case f.Name == "":
		return fmt.Errorf("a file needs a name")
	case len(f.Name) > MaxName:
		return fmt.Errorf("that name is longer than %d characters", MaxName)
	case f.Visibility != VisibilityPrivate && f.Visibility != VisibilityPublic:
		return fmt.Errorf("visibility %q is not %s or %s", f.Visibility, VisibilityPrivate, VisibilityPublic)
	case f.Size < 0:
		return fmt.Errorf("a file is not a negative number of bytes")
	case len(f.SHA256) != 64:
		return fmt.Errorf("a file carries the digest of what arrived")
	case f.StorageKey == "":
		return fmt.Errorf("a file's bytes are somewhere")
	}
	return nil
}

// Storage is where the bytes go. There is one implementation in this module, on
// local disk, and the ones that speak to an object store live outside this
// repository: a reference architecture carrying an S3 client would be teaching
// S3, and the interface is what makes that a wiring decision rather than a
// rewrite.
//
// It takes no transaction, and that is the shape of the whole module: a blob
// write cannot be rolled back, so it happens outside one on purpose and the
// order relative to the commit is chosen at each call site. Every key it is
// given is a UUID this module generated.
type Storage interface {
	// Put writes the bytes at key. size is what the caller declared, or -1 when
	// nothing did; an implementation that has to know a length up front may
	// refuse -1, and the one here ignores it. Writing a key that already exists
	// is an error, because a key is minted per upload and a collision is a bug
	// rather than a replacement.
	Put(ctx context.Context, key string, r io.Reader, size int64) error

	// Get opens the bytes at key, or ErrNoBlob when there are none. The caller
	// closes what it is given.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the bytes at key. A key with nothing at it is not an
	// error: the worker that calls this retries, and a retry that failed
	// because the first attempt succeeded would never stop.
	Delete(ctx context.Context, key string) error
}

// Upload is one arriving file: what the caller said about it, and the bytes.
// The reader is streamed straight to Storage while it is hashed and counted, so
// nothing here is ever held in memory or spooled to a temporary file.
type Upload struct {
	Name        string
	ContentType string
	Visibility  string
	// Declared is the length the request declared, or -1. It is a hint for a
	// Storage that has to know one up front, and nothing else: the size that is
	// stored and the limit that is enforced both come from counting the bytes
	// as they go past, because a request that declares a length can be wrong
	// about it either by accident or on purpose.
	Declared int64
	Body     io.Reader
}

// Service is what a caller does with files. Every command takes the caller's
// transaction rather than opening one, so the row and its event commit
// together; the errors are kit/crud's, plus ErrTooLarge.
type Service interface {
	// Upload streams the bytes into Storage while hashing and counting them,
	// writes the row and publishes file.uploaded.
	//
	// The bytes are written before the row, and the consequence is stated
	// rather than hidden: a transaction that fails after this leaves bytes
	// nobody references, which costs disk. The other order costs a row that
	// references nothing, which is a download that fails forever. Past the
	// deployment's limit it answers ErrTooLarge and removes what it had
	// written, because a caller that was refused should not be charged for
	// storage.
	Upload(ctx context.Context, tx db.Tx[db.Tenant], up Upload) (*File, error)

	// Open is the row and its bytes. anonymous is the public door: it serves
	// only a file whose visibility says anybody may read it, and answers
	// ErrNotFound for everything else, so a private file and a file that does
	// not exist are the same answer to a caller who is not signed in.
	Open(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, anonymous bool) (*File, io.ReadCloser, error)

	// Delete removes the row and publishes file.deleted, carrying the storage
	// key. The bytes are removed by whoever handles that event, after this
	// transaction commits — see the module's subscription. Deleting what is
	// already gone is ErrNotFound, because the row is what a caller named.
	Delete(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*File, error)
}
