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
	EventUploaded = "file.uploaded"
	EventDeleted  = "file.deleted"
	EventRetained = "file.retained"
	EventReleased = "file.released"
	EventErased   = "file.erased"
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
	At      time.Time `json:"at"`
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
// policy, which is the only reason anyone needs to read about it.
type Released struct {
	FileID uuid.UUID `json:"fileId"`
	At     time.Time `json:"at"`
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
}
