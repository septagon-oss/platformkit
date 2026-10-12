package contracts

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// variants.go is the plan one frame becomes, stated as arithmetic and nothing
// else. Both implementations — the SQL service and the conformance fake — call
// these four functions, and neither re-derives any answer from them: a plan held
// in one of the two would be one question with two answers, which is how a
// srcset ends up naming a width no object exists at.
//
// A variant is not a file. It has no row in `files`, so it never appears in a
// file list, is never opened as a file of its own, and is never counted as a use
// the way a body's image is; it is the same picture at one width, and it dies
// with its parent, in its parent's transaction.

// VariantWidthSet is the closed set of widths this kernel serves, ascending. It
// is the answer `?w=` accepts and the only thing a srcset can name; a width
// outside it is a caller bug and the door says so with 422 rather than guessing.
var VariantWidthSet = []int{480, 960, 1440, 1920}

// VariantWidths is the plan for one frame: every served width strictly smaller
// than the frame's own, ascending. "Strictly" is the whole of the never-upscale
// rule — a 700 px frame gets 480 and nothing else, a 480 px frame gets nothing,
// and a 2400 px frame gets four.
//
// The parent's own width is never in the list, because the parent is already
// stored and already served at the bare address; a variant at the frame's width
// would be a second object with nothing to say about it.
func VariantWidths(frameWidth int) []int {
	var out []int
	for _, w := range VariantWidthSet {
		if w < frameWidth {
			out = append(out, w)
		}
	}
	return out
}

// VariantHeight is the other number of a planned variant: the frame's ratio kept
// at the planned width, never rounded down to zero. A 2400x1600 frame at 960 is
// 640 tall; a 3x1 frame at 480 is one pixel tall and not zero, because a
// zero-tall object cannot be encoded and cannot be laid out.
func VariantHeight(frameWidth, frameHeight, width int) int {
	if frameWidth <= 0 || frameHeight <= 0 || width <= 0 {
		return 0
	}
	// Integer rounding rather than truncation: truncation says a very wide
	// banner is nothing at all when it is a pixel tall.
	h := (frameHeight*width + frameWidth/2) / frameWidth
	if h < 1 {
		return 1
	}
	return h
}

// VariantKey is the object name one (file, width) has, forever.
//
// It is derived and not minted, and that is the whole of the encode's
// idempotency: `Storage.Put` refuses a key that exists, so a key minted per
// attempt would make a retried encode fail forever, and a key derived from the
// two inputs makes it write the same object again — which the job does behind a
// Delete, so the retry is a replacement and not a collision.
//
// The value is still a UUID, which is what keeps ErrInvalidKey's rule untouched:
// a key is never caller-supplied, and nothing about a name's provenance changes
// the shape a storage adapter parses.
func VariantKey(fileID uuid.UUID, width int) Key {
	sum := uuid.NewSHA1(uuid.NameSpaceURL, []byte("platformkit:file-variant:"+fileID.String()+":"+strconv.Itoa(width)))
	return Key(sum.String())
}

// The three states a variant row answers. pending → ready is one encode and one
// charge; pending → failed is permanent, with the reason on the row; there is no
// other edge, and a ready row is never rewritten, which is the same write-once
// rule Storage.Put enforces over the bytes.
const (
	VariantPending = "pending"
	VariantReady   = "ready"
	VariantFailed  = "failed"
)

// Why a pending row became failed, as a closed set. A quota refusal is permanent
// by decision: the original was already accepted, charged and served, and a
// variant that cannot fit is a fact about this tenant's disk rather than a
// transient failure worth re-encoding forever.
const (
	VariantFailedDecode = "decode"
	VariantFailedStore  = "store"
	VariantFailedQuota  = "quota"
	VariantFailedGone   = "gone"
)

// Variant is one planned width of one stored image: the plan (the derived key,
// the width), and what the encode made of it once it ran.
//
// A pending row carries no size, no digest and no height. That is what makes
// filing the plan free — the quota is a sum over sizes, and an unplanned zero
// charges nothing — and it is why the state flip and the charge happen in one
// statement: the bytes that were written become part of what this tenant holds
// exactly when they become servable.
type Variant struct {
	crud.Base

	FileID uuid.UUID `json:"fileId" gorm:"column:file_id;type:uuid;not null" format:"uuid" doc:"The file this is a width of" readOnly:"true"`
	Width  int       `json:"width" gorm:"not null" doc:"Pixels across" readOnly:"true" example:"960"`
	Height int       `json:"height" gorm:"not null;default:0" doc:"Pixels down, once encoded" readOnly:"true"`

	// State is pending until an encode writes the bytes, ready when it did, and
	// failed when it will not. Reason is which failure, and only ever non-empty
	// with failed.
	State  string `json:"state" gorm:"type:varchar(8);not null;default:'pending'" enum:"pending,ready,failed" ui:"widget:select" doc:"Whether this width can be served" readOnly:"true" required:"false"`
	Reason string `json:"reason" gorm:"type:varchar(24);not null;default:''" maxLength:"24" ui:"hide:list" doc:"Why this width failed, once it has" readOnly:"true" required:"false"`

	// ContentType, Size and SHA256 describe the object at StorageKey once it
	// exists. The type is the parent's: alpha decided the container once, on the
	// frame, and a variant that re-decided it could disagree with the picture it
	// was scaled from.
	ContentType string `json:"contentType" gorm:"type:varchar(120);not null;default:''" maxLength:"120" ui:"hide:list" readOnly:"true" required:"false"`
	Size        int64  `json:"size" gorm:"not null;default:0" doc:"Bytes of this width" readOnly:"true" required:"false"`
	SHA256      string `json:"sha256" gorm:"type:char(64);not null;default:''" ui:"hide:list" readOnly:"true" required:"false"`

	// StorageKey is derived from (FileID, Width) by VariantKey and written when
	// the row is filed, before any byte exists at it, so a retry needs no state
	// beyond the row.
	StorageKey string `json:"-" gorm:"type:varchar(64);not null"`
}

// TableName pins the table, so the entity and migrations/000054 agree.
func (Variant) TableName() string { return "file_variants" }

// Validate is the same shape file_variants CHECKs, in Go and before the write: a
// width outside the served set is a plan nobody wrote, a ready row without its
// numbers is a lie about bytes that do not exist, and a failed row that does not
// say why leaves a person with nothing to read.
func (v *Variant) Validate(ctx context.Context) error {
	switch {
	case v.FileID == uuid.Nil:
		return fmt.Errorf("a variant names the file it is a width of")
	case !slicesContains(VariantWidthSet, v.Width):
		return fmt.Errorf("%d is not a width this deployment serves", v.Width)
	case v.State != VariantPending && v.State != VariantReady && v.State != VariantFailed:
		return fmt.Errorf("state %q is not %s, %s or %s", v.State, VariantPending, VariantReady, VariantFailed)
	case v.Reason != "" && v.State != VariantFailed:
		return fmt.Errorf("only a failed width has a reason")
	case v.State == VariantFailed && v.Reason == "":
		return fmt.Errorf("a width that failed says why: %s, %s, %s or %s",
			VariantFailedDecode, VariantFailedStore, VariantFailedQuota, VariantFailedGone)
	case v.State == VariantReady && (v.Size <= 0 || len(v.SHA256) != 64 || v.Height <= 0):
		return fmt.Errorf("a ready width carries its bytes, its height and its digest")
	case v.State != VariantReady && v.Size != 0:
		return fmt.Errorf("a width that is not ready charges for nothing")
	case v.Size < 0 || v.Height < 0:
		return fmt.Errorf("a width is not a negative number of bytes or pixels")
	case v.StorageKey == "":
		return fmt.Errorf("a variant's bytes have a name before they have bytes")
	}
	return nil
}

// VariantPlan is the whole plan for one measured frame, as rows: one pending row
// per planned width, keyed by the derivation and nothing else.
//
// Upload files these in the transaction that writes the parent, so a plan never
// exists without the row it belongs to, and a rolled-back upload files nothing.
func VariantPlan(fileID uuid.UUID, frameWidth, frameHeight int, contentType string) []*Variant {
	var out []*Variant
	for _, w := range VariantWidths(frameWidth) {
		out = append(out, &Variant{
			FileID: fileID, Width: w, State: VariantPending, ContentType: contentType,
			StorageKey: VariantKey(fileID, w).String(),
		})
	}
	return out
}

// VariantServable is the read every consumer of a width needs: the ready ones of
// one file, ascending by width. It is what a srcset is built from and what the
// door answers a `?w=` with, so the two cannot disagree about which widths exist.
type VariantServable interface {
	Variants(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID) ([]Variant, error)
}

// WidthOpener is the door that serves one width. Open answers the bare address;
// OpenWidth answers that address with `?w=` on it, and refuses to fall back: one
// address with one width names one object, or every cache in front of the kernel
// becomes a lie about which one it holds.
//
// It is its own interface and not a method on Service, which is how this
// kernel adds a door to something already published: `Opener` is read-only, and
// `Signer`, `Prover` and `Reconciler` are asserted where they are used rather than
// demanded of every implementation. The routes that answer a width are this
// module's own, and internal.RegisterRoutes asks for Service and this beside it, so
// an implementation that encodes nothing — a test double, a client's fake — keeps
// compiling and simply has no widths to serve.
type WidthOpener interface {
	// OpenWidth is Open at one planned width. width 0 is the bare address, and
	// a width the plan never offered is ErrNoBlob: the caller has no width to
	// serve and says so rather than quietly answering with the original.
	OpenWidth(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, anonymous bool, width int) (*File, io.ReadCloser, error)
}

// slicesContains is membership for the small closed sets in this file. It is not
// slices.Contains because this file's sets are []int and importing "slices" for
// one call would be the larger dependency.
func slicesContains(set []int, v int) bool {
	for _, x := range set {
		if x == v {
			return true
		}
	}
	return false
}
