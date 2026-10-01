package contracts

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
)

// MaxHoldReason is the column's width for why a file is being kept.
const MaxHoldReason = 500

// MaxErasureReason is the same width for why a file is being removed. The two
// decisions are one sentence apiece and the limit is the same number, but they
// are checked by different commands against different columns, so they are named
// apart: a hold's reason is validated by the entity and an erasure's by the
// service, and a shared constant would let one command's limit move the other's.
const MaxErasureReason = 500

// The three causes an erasure row names. A closed set and not free text,
// because the thing a person reads a proof table for is "which of these were the
// law, which were a person, and which were the clock running out".
const (
	// EraseExpired is the sweep removing a file whose class ran out.
	EraseExpired = "expired"
	// EraseSubject is a subject-data erasure.
	EraseSubject = "subject"
	// EraseCaller is a delete or an erasure a person asked for.
	EraseCaller = "caller"
)

// kindShape is a class name as the SQL column checks it. The module never
// interprets a kind — it matches it against config and refuses to delete what it
// does not recognise — so all it can check is that the token is a token.
var kindShape = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// ValidKind is the syntax check on a retention class. An empty kind means "no
// class", which is what every upload that says nothing has.
func ValidKind(s string) bool { return s == "" || kindShape.MatchString(s) }

// Hold is why a file must outlive its retention class.
//
// It is a row and not a column on files because two things have to be true at
// once: the hold outlives the policy that would have deleted the file, and
// placing or lifting one is a decision somebody made and said a reason for. A
// boolean on the row would answer neither.
type Hold struct {
	crud.Base

	FileID uuid.UUID `json:"file" gorm:"column:file_id;type:uuid;not null" format:"uuid" doc:"The file held" readOnly:"true"`
	// Until is when the hold stops on its own; NULL means it does not stop
	// until a person releases it, which is what a legal hold is.
	Until *time.Time `json:"until,omitempty" format:"date-time" doc:"When the hold expires, or never" required:"false"`
	// Reason is the sentence a person will read in two years and needs to be
	// able to act on.
	Reason   string    `json:"reason" gorm:"type:text;not null" validate:"required" minLength:"1" maxLength:"500" doc:"Why it is held" example:"court order 2026-0412"`
	PlacedBy uuid.UUID `json:"placedBy,omitempty" gorm:"column:placed_by;type:uuid" format:"uuid" ui:"hide:list" doc:"Who placed it" readOnly:"true"`
}

// TableName pins the table, so the entity and its migration agree.
func (Hold) TableName() string { return "file_holds" }

// Live reports whether the hold stops anything right now.
func (h *Hold) Live(now time.Time) bool { return h.Until == nil || h.Until.After(now) }

// Validate is the entity's own check, run by kit/crud on every write.
func (h *Hold) Validate(_ context.Context) error {
	h.Reason = strings.TrimSpace(h.Reason)
	switch {
	case h.Reason == "":
		return fmt.Errorf("a hold is placed for a reason")
	case len(h.Reason) > MaxHoldReason:
		return fmt.Errorf("a hold's reason is at most %d characters", MaxHoldReason)
	case h.FileID == uuid.Nil:
		return fmt.Errorf("a hold names the file it holds")
	}
	return nil
}

// Erasure is one file's proof that its bytes are gone.
//
// It carries the storage key, the digest and the size it removed because the row
// it proofs is deleted by the same command: by the time anybody reads this table
// there is nothing left to join to. That is the same reason Deleted carries the
// storage key today.
//
// VerifiedAt is the whole of the promise, and it is NULL until the store has
// answered that it holds nothing at that name — no object, no version, no delete
// marker, no abandoned multipart part. A row that stamped it anyway would be a
// certificate this module signed without checking.
type Erasure struct {
	crud.Base

	FileID     uuid.UUID `json:"file" gorm:"column:file_id;type:uuid;not null" format:"uuid" doc:"The file whose bytes were removed" readOnly:"true"`
	StorageKey string    `json:"storageKey" gorm:"column:storage_key;type:varchar(64);not null" doc:"Where the bytes were" readOnly:"true"`
	SHA256     string    `json:"sha256" gorm:"type:char(64);not null" doc:"Digest of what was removed" readOnly:"true"`
	Size       int64     `json:"size" gorm:"not null" doc:"Bytes removed" readOnly:"true"`
	Cause      string    `json:"cause" gorm:"type:varchar(16);not null" enum:"expired,subject,caller" doc:"Why" readOnly:"true"`
	// Reason is the sentence that came with the request, when one did — the
	// subject's erasure route takes it, the sweep has a class and a date instead,
	// and a plain delete may have had nothing. '' is nobody said why, which is a
	// fact about the request and not a missing value.
	Reason string `json:"reason,omitempty" gorm:"type:text;not null;default:''" doc:"The reason the removal was asked for" readOnly:"true" maxLength:"500"`

	// Subject is set for a subject-data erasure and NULL otherwise, so that the
	// one question a data-protection officer asks — "what did you remove for
	// this person?" — is a WHERE clause and not a guess.
	SubjectID uuid.UUID `json:"subject,omitempty" gorm:"column:subject_id;type:uuid" format:"uuid" ui:"hide:list" doc:"The subject this erasure is about" readOnly:"true"`
	// Actor is who asked; NULL for the sweep, which has no actor.
	Actor uuid.UUID `json:"actor,omitempty" gorm:"type:uuid" format:"uuid" ui:"hide:list" doc:"Who asked for it" readOnly:"true"`

	RemovedAt  time.Time  `json:"removedAt" format:"date-time" doc:"When the store said it had nothing" readOnly:"true"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty" format:"date-time" doc:"When absence was checked; never checked while NULL" readOnly:"true"`
	// VersionsSeen is what the listing counted. A store that keeps versions
	// cannot pass this table silently: the number is above zero and the erasure
	// is refused as unprovable rather than certified.
	VersionsSeen int `json:"versionsSeen" gorm:"column:versions_seen;not null;default:0" doc:"Versions, delete markers and parts the store reported" readOnly:"true"`
}

// TableName pins the table, so the entity and its migration agree.
func (Erasure) TableName() string { return "file_erasures" }

// ErasureReceipt is what one erasure command did.
type ErasureReceipt struct {
	Subject uuid.UUID `json:"subject" format:"uuid"`
	Files   int       `json:"files"`
	Bytes   int64     `json:"bytes"`
	// Held lists the files a live hold kept, which the command refuses to
	// remove and says so about rather than quietly skipping them.
	Held []uuid.UUID `json:"held,omitempty"`
	At   time.Time   `json:"at" format:"date-time"`
}
