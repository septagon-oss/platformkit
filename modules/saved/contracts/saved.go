// Package contracts specifies personal retained copies and their source boundary.
// All service calls use the caller's tenant transaction and return zero on error.
package contracts

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	picture "github.com/septagon-oss/platformkit/modules/picture/contracts"
)

const (
	PermissionRead  = "saved:read"
	PermissionWrite = "saved:write"
	EventSaved      = "saved.saved"
	EventUnsaved    = "saved.unsaved"
)

var (
	ErrInvalid     = errors.New("saved: invalid input")                  // Correctable: supply valid input.
	ErrDenied      = errors.New("saved: permission denied")              // Correctable: obtain authority.
	ErrNotFound    = errors.New("saved: item not found")                 // Immutable: outside this person's scope.
	ErrStale       = errors.New("saved: stale revision")                 // Correctable: read again before a new intent.
	ErrUnavailable = errors.New("saved: source unavailable")             // Immutable: the named source is gone.
	ErrTemporary   = errors.New("saved: source temporarily unavailable") // Correctable: retry after recovery.
)

// Ref is a source's canonical identity. Module and Kind are lower-case slugs;
// Key is opaque UTF-8, at most 512 bytes. Saved never normalizes any part.
type Ref struct {
	Module string `json:"module"`
	Kind   string `json:"kind"`
	Key    string `json:"key"`
}

// PictureInput names either an immutable file or a compiled composed picture.
// Its zero value means no picture. FileID and Template are mutually exclusive.
type PictureInput struct {
	FileID   string
	Template picture.Template
	Value    picture.Value
}

// Capture is source-owned data. Version is the source revision, never the saved
// row revision. Values contains a complete JSON object, not instructions to rerun.
type Capture struct {
	Title   string
	Locale  string
	Version string
	Values  json.RawMessage
	Picture PictureInput
}

// Picture is a retained image. SVG comes only from the picture compositor;
// FileID is held through the existing file-use ledger. Neither grants access.
type Picture struct {
	FileID         string          `json:"fileId,omitempty"`
	SVG            string          `json:"-"`
	TemplateDigest string          `json:"templateDigest,omitempty"`
	Value          json.RawMessage `json:"value,omitempty"`
}

// Snapshot retains the first successful capture unchanged until removal.
// Its language is captured too; later UI language changes do not translate it.
type Snapshot struct {
	Title         string          `json:"title"`
	Locale        string          `json:"locale"`
	SourceVersion string          `json:"sourceVersion"`
	Values        json.RawMessage `json:"values"`
	Picture       Picture         `json:"picture"`
	CapturedAt    time.Time       `json:"capturedAt"`
}

// Item is one person's immutable copy. Revision is 1 for its whole lifetime;
// removing and saving again creates a new ID, never an update of the old copy.
type Item struct {
	entity.Base
	PersonID string   `json:"-"`
	Source   Ref      `json:"source"`
	Revision int64    `json:"revision"`
	Snapshot Snapshot `json:"snapshot"`
}

// TableName identifies the sole saved-owned table.
func (*Item) TableName() string { return "saved_items" }

// Save quotes the source version observed by the caller. Capture reauthorizes
// and compares that version even on a duplicate; no snapshot is client supplied.
type Save struct {
	Source                Ref    `json:"source"`
	ExpectedSourceVersion string `json:"expectedSourceVersion"`
}

// Unsave names a saved row and its observed revision. Missing, already removed
// and foreign rows all succeed without a write; a present own row checks revision.
type Unsave struct {
	ID               string `json:"id"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

// Query applies exact Module and Kind filters before counting and paging.
// Empty filters mean all; Limit 0 means 24, with a maximum of 100; Offset is >=0.
type Query struct {
	Module string
	Kind   string
	Limit  int
	Offset int
}

// View adds a live observation without changing the retained item. Availability
// is available, unavailable or temporary; it is never persisted as domain state.
type View struct {
	Item
	Availability string `json:"availability"`
}

type Page struct {
	Items []View `json:"items"`
	Total int64  `json:"total"`
}

// Source runs in the supplied transaction under the current actor. Capture
// authorizes retaining all values/media and checks expectedVersion while holding
// the source's consistency locks. Check and Open reauthorize current live access.
// Every refusal returns zero. These calls make no external or local mutation.
type Source interface {
	Capture(ctx context.Context, tx db.Tx[db.Tenant], key, expectedVersion string) (Capture, error)
	Check(ctx context.Context, tx db.Tx[db.Tenant], key string) error
	Open(ctx context.Context, tx db.Tx[db.Tenant], key string) (string, error)
}

// Words are declaration-owned English fallbacks, localized through the existing
// entry word catalogue's derived keys, never by the source callbacks.
type Words struct {
	Save      string
	Saved     string
	Remove    string
	ListTitle string
}

// Savable is a typed pkit contribution for one module/kind, frozen at composition.
// Source must be non-nil; duplicate pairs refuse composition before opening IO.
type Savable struct {
	Module string
	Kind   string
	Words  Words
	Source Source
}

// Clock is the composition's own now: the instant a capture is retained at, and
// so the order one person's copies come back in. NewService leaves it unset, which
// is the kernel's process clock; the module's boundary requires one, so nothing a
// composition builds answers with the default, and a test supplies one it can move.
type Clock func() time.Time

// Access rereads the authenticated person's current membership and grant in tx.
// It returns a canonical person UUID string, or zero with a refusal. Cached
// request grants cannot substitute for this check after a lock wait. Authority
// locks live until commit; implementations must obey the documented lock order.
type Access interface {
	Current(context.Context, db.Tx[db.Tenant], string) (string, error)
}

// Service preserves private snapshots until Unsave or personal-data erasure.
// Mutations must roll back on any error. Save is naturally idempotent while the
// item exists; a delayed Save after Unsave can create a fresh copy. See README.
type Service interface {
	Save(context.Context, db.Tx[db.Tenant], Save) (Item, error)
	Unsave(context.Context, db.Tx[db.Tenant], Unsave) error
	Get(context.Context, db.Tx[db.Tenant], string) (View, error)
	Find(context.Context, db.Tx[db.Tenant], Ref) (View, error)
	List(context.Context, db.Tx[db.Tenant], Query) (Page, error)
	Open(context.Context, db.Tx[db.Tenant], string) (string, error)
}

// Changed is the minimal outbox payload. It carries no source key, person ID,
// title, image or values; the event envelope supplies tenant, actor and time.
type Changed struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}
