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
	"fmt"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

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
//
// MaxContentType is the column's width, and it is checked here so that a
// header somebody padded to a kilobyte is a 422 the caller can act on rather
// than a constraint violation the database raises as a 500.
const (
	MaxName        = 255
	MaxContentType = 120
	// MaxAlt is how much a picture is allowed to say about itself. The column is
	// varchar(1000) and the entity refuses 1001 characters, so the answer is a
	// 422 that names the limit and not a constraint violation arriving as a 500.
	MaxAlt = 1000
)

// Renderable is the closed set of media types this application will let a
// browser display rather than download. Everything else is served as an
// attachment, which is what makes an uploaded document somebody else's file
// instead of a script on this tenant's origin.
//
// It is a list of what is safe and not a list of what is not, because the
// dangerous set is open: text/html, image/svg+xml, application/xhtml+xml and
// every XML dialect a browser will execute a script inside are the ones anybody
// thinks of, and the next browser adds another. Three raster formats, two that
// are effectively raster, PDF and plain text are what a product actually needs
// to show inline.
//
// PDF is in it, and that is a judgement rather than an oversight: a PDF can
// carry JavaScript, but the built-in viewer of every current browser runs it in
// a sandbox of its own, and the download below sends a policy that allows
// nothing anyway.
var renderable = []string{
	"image/png", "image/jpeg", "image/gif", "image/webp", "image/avif",
	"application/pdf", "text/plain", "video/mp4", "video/webm", "text/vtt",
}

// Renderable reports whether a stored media type may be served inline. The
// parameters — a charset, a boundary — are not part of the decision, so they
// are stripped first: "text/html; charset=utf-8" is text/html.
func Renderable(contentType string) bool {
	essence, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return slices.Contains(renderable, essence)
}

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
	//
	// It is the audience this row carries, and only one thing ever raises it: the
	// upload. The write that takes an audience back lowers it (see
	// internal.Service.settleAudience), and the derived half of who may read the
	// file — a record with a public face showing it — is never written here,
	// because that half belongs to the record and lives in its use row. An
	// earlier release did write it here, which is what UploadVisibility is for.
	Visibility string `json:"visibility" gorm:"type:varchar(10);not null;default:'private'" enum:"private,public" ui:"widget:select" doc:"Who may read it" default:"private" required:"false"`

	// UploadVisibility is the audience the upload itself was given: the choice
	// made by whoever held file:manage when the bytes arrived, and the floor of
	// who may read the file. No command that shows a file, publishes a record or
	// takes an audience back writes it, which is the whole reason it is a second
	// column and not files.visibility — that one held the derived answer for
	// every file a published page showed, and so stopped saying what its uploader
	// chose. Rows written before this column existed get it back from their own
	// file.uploaded event, or from what their uses account for; see 000064. It is
	// the module's own record of a choice, not a second answer to show a caller:
	// the answer a caller is given is Visibility, that choice widened by what shows it.
	UploadVisibility string `json:"-" gorm:"column:upload_visibility;type:varchar(10);not null;default:'private'"`

	// UploaderID is whoever uploaded it, a user id with no foreign key behind
	// it. Validate stamps it from the caller on the context, the way content
	// stamps an author.
	UploaderID uuid.UUID `json:"uploader,omitempty" gorm:"column:uploader_id;type:uuid" format:"uuid" ui:"hide:list" doc:"The user who uploaded it" readOnly:"true"`

	// Width and Height are the frame's pixels as they are stored — after the
	// image pass turned a rotated frame upright, which is why they describe what
	// a reader gets rather than what the sensor captured. Both are the server's
	// account: no command takes them, and the rich-text port lays out against
	// them instead of decoding a header on every render.
	//
	// Zero means nobody measured: a file the pass did not run over — a PDF, a
	// ZIP, a raster no decoder read — is 0 x 0, and 0 is never a real width.
	Width  int `json:"width" gorm:"not null;default:0" doc:"Pixels across, as stored" readOnly:"true" required:"false" example:"1440"`
	Height int `json:"height" gorm:"not null;default:0" doc:"Pixels down, as stored" readOnly:"true" required:"false" example:"900"`

	// Kind is the retention class the upload arrived under, as a product names
	// it. It is read by exactly one thing — the retention sweep's policy lookup
	// — and by nothing else, which is why it is empty for every upload that
	// does not name one. An unknown kind is never deleted, never guessed at.
	Kind string `json:"kind" gorm:"type:varchar(32);not null;default:''" maxLength:"32" ui:"hide:list" doc:"Retention class, as the product names it" required:"false"`

	// Sealed says these bytes have no door of this module: no content route streams
	// them and no presigned URL opens them. It is the server's account, stamped by
	// the Upload that stored the bytes, and no command and no form field sets it —
	// the same rule Width and Height follow, for the same reason.
	//
	// It exists for the bytes another module stores on its own authority and hands
	// out through its own door: a person's export bundle, which the module that made
	// it releases to one person through one single-use token, and which is not the
	// kind of object the tenant's ordinary file reader may walk up to and download.
	// The row stays listed, because the tenant's record of what it holds is not a
	// secret from the tenant; the door beside it is closed to everybody but the
	// module that sealed it.
	Sealed bool `json:"sealed" gorm:"column:sealed;not null;default:false" doc:"Whether only the module that stored it serves its bytes" readOnly:"true" required:"false" ui:"hide:list"`

	// UnusedSince is when this image stopped being shown by anything: the moment
	// its last use ended. It is the grace period's start, stamped by SetUses when it
	// ends a file's last use, cleared by SetUses when a use restarts, and read by the
	// release sweep, which removes the row once the deployment's grace has run with
	// nobody asking for it. Nothing stamps an arrival: the rule releases a file whose
	// last use *ended*, and a file the ledger has never spoken about has no such
	// moment to measure from — the site logo is named by a bare uuid outside the
	// ledger, so a stamp at upload would reap a configured logo a day after it was
	// saved. NULL therefore means the file is shown, or that the ledger says nothing
	// about it — see the CHECK in migrations/000055, which keeps a file the image
	// pass never measured from carrying a stamp at all.
	UnusedSince *time.Time `json:"unusedSince,omitempty" gorm:"column:unused_since" doc:"Since when nothing shows it" readOnly:"true" required:"false" format:"date-time"`

	// Alt is what the picture says about itself, in the words a person chose, and
	// it is written by one command only — SetAlt — never merged in from a request
	// body. A file may arrive without it; a body may not be handed the file until
	// it has it, or until the picture is declared decorative.
	Alt string `json:"alt" gorm:"column:alt;type:varchar(1000);not null;default:''" maxLength:"1000" doc:"What the image says about itself, empty until somebody captions it"`
	// Decorative is the person saying this picture carries no information a reader
	// would want: the alternative to writing alt text, and never both at once.
	Decorative bool `json:"decorative" gorm:"column:decorative;not null;default:false" doc:"True when the image carries no information a reader would want"`
}

// TableName pins the table, so the entity and migrations/000019 agree.
func (File) TableName() string { return "files" }

// Public reports whether anybody may read this file.
func (f *File) Public() bool { return f.Visibility == VisibilityPublic }

// GivenPublic is the floor: the audience the upload was given, whatever the records
// showing the file have since asserted. Every file a person has ever published reads
// the same as Public(); the two part company for a file whose visibility a record with
// a public face is widening right now, which is readable and was not given public.
func (f *File) GivenPublic() bool { return f.UploadVisibility == VisibilityPublic }

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
	if f.UploadVisibility == "" {
		// A write that names the audience it gives names the floor with the same
		// word: the two columns part company only when something takes an audience
		// back, and no command that does that arrives here.
		f.UploadVisibility = f.Visibility
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
	// Characters, not bytes. len() on a UTF-8 string counts bytes, so a name
	// of 90 emoji used to be refused as 360 characters and a 255-byte column
	// used to be handed 255 characters that did not fit. The same correction
	// is in modules/content, modules/site and modules/billing.
	case utf8.RuneCountInString(f.Name) > MaxName:
		return fmt.Errorf("that name is longer than %d characters", MaxName)
	// The column is varchar(120), so this is the difference between a 422 that
	// says what is wrong and a 500 from the database.
	case utf8.RuneCountInString(f.ContentType) > MaxContentType:
		return fmt.Errorf("a media type is at most %d characters, and that one is %d", MaxContentType, utf8.RuneCountInString(f.ContentType))
	case !validMediaType(f.ContentType):
		return fmt.Errorf("%q is not a media type", f.ContentType)
	case f.Visibility != VisibilityPrivate && f.Visibility != VisibilityPublic:
		return fmt.Errorf("visibility %q is not %s or %s", f.Visibility, VisibilityPrivate, VisibilityPublic)
	// The same shape files_kind CHECKs, so a mistake is a 422 that names the
	// token and not a 500 from the database. This module reads no meaning into
	// a kind — it matches the token against the deployment's table and deletes
	// nothing it does not find there — so syntax is all it can refuse.
	case !ValidKind(f.Kind):
		return fmt.Errorf("%q is not a retention class", f.Kind)
	// The stamp is a claim about the ledger, and the ledger only speaks about
	// pictures the image pass measured. A stamp on an unmeasured row would be the
	// sweep deleting a document upload a day after it arrived.
	case f.UnusedSince != nil && f.Width == 0:
		return fmt.Errorf("nothing this module's ledger can speak about is stamped: an unstored frame carries no grace period")
	case f.Size < 0:
		return fmt.Errorf("a file is not a negative number of bytes")
	// A caption is the entity's business too: whichever door writes it — SetAlt,
	// a fixture, an import — the row may not carry words and the claim that there
	// are none at the same time. The empty-but-not-declarative state is legal to
	// arrive in and is refused by the commands that leave it, which is what
	// contracts.AltClaim's own comment says and what SetAlt enforces.
	case f.Alt != "" && f.Decorative:
		return fmt.Errorf("a decorative image says nothing about itself, and that one carries %d characters", utf8.RuneCountInString(f.Alt))
	case utf8.RuneCountInString(f.Alt) > MaxAlt:
		return fmt.Errorf("an alt text is at most %d characters, and that one is %d", MaxAlt, utf8.RuneCountInString(f.Alt))
	// The same shape files_dimensions CHECKs: a frame is either unmeasured, in
	// which case both numbers are 0, or it is measured, in which case neither
	// side is. A width with no height is a number nobody read.
	case f.Width < 0 || f.Height < 0:
		return fmt.Errorf("a frame is not a negative number of pixels")
	case (f.Width == 0) != (f.Height == 0):
		return fmt.Errorf("a measured frame has both a width and a height")
	case len(f.SHA256) != 64:
		return fmt.Errorf("a file carries the digest of what arrived")
	case f.StorageKey == "":
		return fmt.Errorf("a file's bytes are somewhere")
	}
	return nil
}

// Agrees refuses an upload whose declared type is one the download would render
// inline and whose bytes are something else. head is the first 512 bytes, which
// is what http.DetectContentType reads.
//
// It checks only the renderable types, and the asymmetry is the point: a file
// served as an attachment is somebody else's problem to open, so what it really
// is does not matter here, while a file served inline runs on this tenant's
// origin. The sniffer is allowed to have no opinion — it knows nothing about
// AVIF, and half the world's images are formats it was never taught — but when
// it does have one and it disagrees, the upload is refused.
func Agrees(declared string, head []byte) error {
	if !Renderable(declared) {
		return nil
	}
	want, _, err := mime.ParseMediaType(declared)
	if err != nil {
		return nil // Validate has the last word on the syntax
	}
	// WebVTT is UTF-8 text with a signature, not generic text/plain. Check the
	// header only: the browser parses cues, and the editor verifies their timing.
	if want == "text/vtt" {
		header, _, _ := strings.Cut(strings.TrimPrefix(string(head), "\ufeff"), "\n")
		header = strings.TrimSuffix(header, "\r")
		if (header == "WEBVTT" || strings.HasPrefix(header, "WEBVTT ") || strings.HasPrefix(header, "WEBVTT\t")) && !strings.Contains(header, "-->") {
			return nil
		}
		return fmt.Errorf("%w: captions need a WebVTT header", crud.ErrInvalid)
	}
	got, _, err := mime.ParseMediaType(http.DetectContentType(head))
	if (want == "video/mp4" || want == "video/webm") && got != want {
		return fmt.Errorf("%w: video bytes do not match %s", crud.ErrInvalid, want)
	}
	if err != nil || got == "application/octet-stream" || got == want {
		return nil
	}
	return fmt.Errorf("%w: this was uploaded as %s and its bytes are %s; a file that would be shown in the browser has to be what it says it is",
		crud.ErrInvalid, want, got)
}

// validMediaType is the syntax check: type/subtype, with parameters this
// application does not read but a browser might. It is deliberately only a
// syntax check — what the bytes actually are is checked at upload, against
// http.DetectContentType, and only for the types that would be rendered.
func validMediaType(s string) bool {
	_, _, err := mime.ParseMediaType(s)
	return err == nil
}

// Service is what a caller does with files. Every command takes the caller's
// transaction rather than opening one, so the row and its event commit
// together; the errors are kit/crud's, plus ErrTooLarge.
//
// It does not carry the width door: OpenWidth is contracts.WidthOpener, a type
// beside this one. This interface is what a *caller* does with files, and a
// caller outside this module serves no `?w=` — the only route that answers a
// width is this module's own. The other reason is checkable rather than
// stylistic: an exported interface that gains a method is a break for anybody
// outside this repository who implemented it, and `Opener`, `Signer` and `Prover`
// are each separate types for exactly that reason.
type Service interface {
	Opener

	// Upload streams the bytes into Storage while hashing and counting them,
	// then opens the caller's transaction, writes the row and publishes
	// file.uploaded.
	//
	// The bytes are written before the row, and the consequence is stated
	// rather than hidden: a transaction that fails after this leaves bytes
	// nobody references, which costs disk. The other order costs a row that
	// references nothing, which is a download that fails forever. Past the
	// deployment's limit it answers ErrTooLarge, and past what the tenant's
	// quota has room for ErrQuota; either way it removes what it had written,
	// because a caller that was refused should not be charged for storage.
	//
	// The quota is measured inside the transaction and under a lock on the
	// tenant, so twenty uploads that start together cannot all read the same
	// total and all decide there is room — which is what the review measured at
	// 2.9 times the quota.
	Upload(ctx context.Context, tx Tx, up Upload) (*File, error)

	// Delete removes the row and publishes file.deleted, carrying the storage
	// key. The bytes are removed by whoever handles that event, after this
	// transaction commits — see the module's subscription. Deleting what is
	// already gone is ErrNotFound, because the row is what a caller named.
	//
	// A file some record still shows is refused with ErrInUse naming how many
	// records show it, and nothing is written, published or removed. The check and
	// the removal are one transaction under a lock on the file row, so a use that
	// begins concurrently is decided rather than raced: it waits behind the removal
	// and is then refused the file it named, or it lands first and is counted.
	Delete(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*File, error)

	// Grant hands out a time-limited door onto one row's bytes, so a private
	// byte can be read with no request of this process in the path — which is
	// what a native client wants, and what anything that hands a link to a
	// person wants. It changes no state, so it publishes nothing and writes no
	// audit row: a trail of every link ever minted is noise, and the mint is
	// logged with the trace id instead.
	//
	// A public file is refused with ErrPublicFile rather than answered: it
	// already has an open door, and handing out a signature for an object that
	// needs none teaches that signing is where the access control lives.
	Grant(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, expiry time.Duration) (*Grant, error)

	// Retain places or replaces a hold, which is how a person or an obligation
	// says a file must outlive its class. until NULL means held until released.
	Retain(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID, until *time.Time, reason string) (*Hold, error)

	// Release removes the hold, which returns the file to its class's policy.
	Release(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID) error

	// Uses lists the live uses of one file — which record's field, in which
	// locale. It is the details panel's answer and the sweep's question, and it
	// is RLS-scoped like every other read here: a file another tenant holds
	// answers ErrNotFound, not an empty list, because an empty list would say
	// "nobody reads this one" about a file the reader cannot see at all.
	Uses(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID) ([]UseRow, error)

	// SetUses rewrites the uses one record field makes, inside the writer's
	// transaction, from the references its body carries. refs is the ids in
	// document order, and repeats collapse to one use.
	//
	// It is a rewrite and not an append, because the body is the whole truth
	// about what a record shows: a use that survived the edit that dropped the
	// image is a use of a file nothing displays, which is exactly the row the
	// release sweep would then be wrong to remove.
	//
	// Every file it names is locked before its rows are written, in ascending id
	// order so two records that share two files cannot lock them in opposite
	// orders. A file whose use ends is locked FOR UPDATE and one the new set only
	// names FOR KEY SHARE, because "this file is read by nothing now" is a count of
	// rows another rewrite may be ending in the same instant: two rewrites that drop
	// the last two uses of one picture serialise on it and the later one counts after
	// the earlier one lands, so the one that really ended the last use starts the
	// grace period. A file the new set names that is gone answers ErrNotFound
	// naming the id — a use of a file that no longer exists is never written,
	// and nothing else in the set is written either. A file this rewrite only
	// ends has no such refusal: its ledger row was filed while the file existed
	// and the file may have been removed since, which is the row to end rather
	// than a reason to refuse. A record would otherwise be undeletable once
	// somebody deleted its image, and the row saying a gone file is still shown
	// would outlive every body that ever named it. It returns the ids whose last use
	// this ended, which is the list the release sweep is handed, and the stamp the
	// sweep measures its grace period from: the ids it returns have their
	// unused_since set in this same transaction, and a file whose use restarts has the
	// stamp cleared.
	//
	// One publication is its own, and it is the file's. An ended row that had asserted
	// a public audience takes that audience back here, in the transaction that ended
	// it, because this rewrite is what every delete and every edit goes through: a
	// record that was showing a picture is gone, and its bytes must not stay readable
	// by anybody because nobody recomputed them. The rule is SetShown's — a stranger
	// may read the file when any live use of it is public or when its own visibility
	// says so — and so is its exemption: a file the ledger never had a row for is not
	// this command's to change, and neither is one whose ended rows said
	// nothing about the public. Only an audience a publish granted is a publish's to
	// take back: what an upload was given is the floor, and no record's edit narrows
	// it. Every other fact stays silent: the record's own write is the auditable
	// one, and the file module has no business emitting an event for somebody else's
	// row.
	SetUses(ctx context.Context, tx db.Tx[db.Tenant], use Use, refs []uuid.UUID) ([]uuid.UUID, error)

	// SetShown says which files one record is showing, and whether it is showing
	// them to anybody. owner names the record — its module, entity and id — with
	// Field and Locale left empty, meaning every field and every locale of it.
	//
	// It is the audience half of the ledger, and the half that is asserted: the
	// module that owns the record's public face calls it from the command that
	// moves the lifecycle, in that command's transaction, so the flip and the move
	// commit together or not at all. Each named file is locked FOR UPDATE, in
	// ascending id order, before its rows are written and the answer recomputed from
	// the whole ledger: somebody who has not signed in may read it when any live use of
	// it is public, or when its own visibility says so. What is recomputed is that
	// answer, published as a flip and asked again at the door — never the file's own
	// visibility, which is what its upload was given and is no command's to take back.
	// A file the ledger does not speak about is not named here and keeps whatever its
	// upload asked for.
	//
	// public true with no use row for one of the refs is refused — the bytes would
	// go public while nothing records who shows them, so nothing could ever turn
	// them back. public false never refuses: taking an image away from the public
	// is the safe direction. It publishes file.visibility for each flip it makes,
	// and nothing for a file whose visibility was already right.
	SetShown(ctx context.Context, tx db.Tx[db.Tenant], owner Use, refs []uuid.UUID, public bool) error

	// EraseSubject removes one subject's rows and bytes for this tenant, under
	// audit: the rows go in this transaction, the bytes go in the worker that
	// handles the event, and the proof row records the digest that is gone.
	//
	// A subject with no rows is not an error. It answers a receipt with zero
	// counts, publishes nothing and writes nothing, because an erasure of
	// nothing is an answer rather than a thing done.
	//
	// The reason is why the person asked. It is filed, not accepted and dropped:
	// it travels with each removal and lands in the proof row beside the digest,
	// which is the record a data-protection question is answered from. Empty is
	// allowed; past MaxErasureReason is refused before anything is removed.
	EraseSubject(ctx context.Context, tx db.Tx[db.Tenant], subject uuid.UUID, reason string) (*ErasureReceipt, error)
}

// DefaultGrantExpiry is the life of a grant nobody asked a length for: long
// enough to open a document on a slow connection, short enough that a leaked
// link is a bad hour and not a bad forever.
const DefaultGrantExpiry = 15 * time.Minute

// MaxGrantExpiry is the longest grant this module will mint. It is inside the
// seven days an S3 signature may live, so the module's own ceiling is the one
// that binds. Past it a request is refused and not clamped: a caller that asked
// for a day and silently got fifteen minutes would ship a link that dies in the
// middle of somebody's download.
const MaxGrantExpiry = 24 * time.Hour

// Is lets errors.Is find ErrInUse in a refusal that carries its size.
func (e *InUse) Is(target error) bool { return target == ErrInUse }
