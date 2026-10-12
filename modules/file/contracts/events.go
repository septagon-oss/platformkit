package contracts

import (
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events"
)

// The five events this module emits. There is no rest.Spec here, so there is no
// file.file.created either: a file arrives as bytes and leaves as bytes, and the
// routes that do it publish these.
//
// The last three are the retention half, and they are the same shape as
// file.deleted for one reason: a blob is one unit of work, it is claimed once,
// and it is retried on its own. So a subject erasure that names forty files
// publishes forty work orders rather than one list of forty, and a delivery that
// dies on the eleventh retries the eleventh instead of redoing ten. Erased is the
// record of each one: what it was, its digest, and what the store said when it
// was asked whether anything was left.
const (
	EventUploaded   = "file.uploaded"
	EventDeleted    = "file.deleted"
	EventRetained   = "file.retained"
	EventReleased   = "file.released"
	EventErased     = "file.erased"
	EventVisibility = "file.visibility"
)

// Events is every event this module emits, for the manifest. kit/app refuses a
// published name that is not in this list, so a fifth event added to the code and
// not to the slice fails at boot rather than traveling unannounced.
var Events = []events.Declared{
	events.Declare[Uploaded](EventUploaded),
	events.Declare[Deleted](EventDeleted),
	events.Declare[Retained](EventRetained),
	events.Declare[Released](EventReleased),
	events.Declare[Erased](EventErased),
	events.Declare[Visibility](EventVisibility),
}

// Uploaded is the payload of EventUploaded. It carries the digest as well as
// the size because the subscriber this exists for is whatever indexes or scans
// an upload, and both are things it would otherwise read the row back for.
type Uploaded struct {
	FileID      uuid.UUID `json:"fileId"`
	Name        string    `json:"name"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	Visibility  string    `json:"visibility"`
	At          time.Time `json:"at"`
}

// Deleted is the payload of EventDeleted, and it carries the storage key
// because by the time anybody handles it the row is gone. That is the whole
// reason this event exists: removing the bytes is work that has to happen after
// the transaction that removed the row commits, and an event is the only thing
// in this architecture that is delivered exactly then.
type Deleted struct {
	FileID     uuid.UUID `json:"fileId"`
	StorageKey string    `json:"storageKey"`
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	// Cause is which of the three removals this was, and Subject is set when it
	// was a subject's. The worker that removes the bytes cannot read the row —
	// this event exists because the row is gone — so the cause of the removal
	// travels with it or it is lost, and the proof row needs it.
	Cause   string    `json:"cause"`
	Subject uuid.UUID `json:"subject,omitempty"`
	// VariantKeys names the widths of this picture, if it had any, and travels for
	// the same reason the storage key does — the rows are gone by the time anybody
	// reads this. A width is a separate object under a name of its own, so nothing
	// but this list can tell a worker what to remove beside the frame; a removal
	// that left four widths behind in the bucket would be an erasure that erased
	// the list and not the picture.
	VariantKeys []string `json:"variantKeys,omitempty"`
	// Reason is the sentence a person gave when they asked for this removal, and
	// it travels for the same reason the storage key does: the row is gone by the
	// time anybody reads this, and the worker that removes the bytes is the one
	// that writes the proof. Empty for the sweep, whose reason is a class and a
	// date, and for a delete nobody explained.
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

// Retained is the payload of EventRetained: a clock somebody stopped, and the
// reason that stopped it, which is the part a reader in two years needs.
type Retained struct {
	FileID uuid.UUID  `json:"fileId"`
	Until  *time.Time `json:"until,omitempty" format:"date-time"`
	Reason string     `json:"reason"`
	At     time.Time  `json:"at"`
}

// Released is the payload of EventReleased. The file goes back to its class's
// policy, which is the only reason anyone needs to read about it — except for the
// second cause, which is named here because the two read differently: a hold
// lifted is a file that stays, and a file released because nothing shows it
// anymore is a row that has gone with its bytes.
type Released struct {
	FileID uuid.UUID `json:"fileId"`
	// Cause is which of the two removals this announces: EraseHold for a hold a
	// person lifted, EraseUnused for the sweep that reaped a file nobody showed.
	Cause string    `json:"cause"`
	At    time.Time `json:"at"`
}

// Visibility is the payload of EventVisibility: one file's audience, as the
// ledger recomputed it, and the record whose use of it changed. It is the
// flip's audit row — the trail keeps the event, its subject, the actor and the
// trace, and the payload is what says why: which record started or stopped
// showing this picture. A flip that changes nothing publishes nothing, so one
// event is one change of audience and not one publish.
type Visibility struct {
	FileID     uuid.UUID `json:"fileId"`
	Visibility string    `json:"visibility"`
	Module     string    `json:"module"`
	Entity     string    `json:"entity"`
	Record     uuid.UUID `json:"record"`
	At         time.Time `json:"at"`
}

// Erased is the payload of EventErased, and it is the record an audit trail
// keeps: what was removed, from where, its digest, and what the store reported
// when asked whether anything was left. versionsSeen is the honest part — a
// store that keeps copies reports a number above zero, and the erasure is
// refused as unprovable rather than certified.
type Erased struct {
	FileID       uuid.UUID  `json:"fileId"`
	StorageKey   string     `json:"storageKey"`
	SHA256       string     `json:"sha256"`
	Size         int64      `json:"size"`
	Cause        string     `json:"cause"`
	Subject      uuid.UUID  `json:"subject,omitempty"`
	Actor        uuid.UUID  `json:"actor,omitempty"`
	VersionsSeen int        `json:"versionsSeen"`
	VerifiedAt   *time.Time `json:"verifiedAt,omitempty" format:"date-time"`
	// Reason is why a person asked, copied through from the removal this proof
	// certifies: the question "why was this person's file removed" is asked of a
	// proof table and not of a row that no longer exists.
	Reason string `json:"reason,omitempty"`
}
