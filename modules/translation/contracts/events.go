package contracts

import (
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events"
)

// EventUpdated is the one event this module emits. One name, and not one per
// verb, because what a subscriber acts on is "this field's text in this
// language changed" — whether a person typed it, a machine drafted it, a
// reviewer accepted it or a delete removed it is the payload's Status and
// Origin, not a different channel to subscribe to.
//
// modules/audit subscribes to every declared event, so this one name is also
// the whole audit trail of every translation in the installation: no audit call
// is written here, and writing one would be a second account of what already
// happened.
const EventUpdated = "translation.updated"

// Updated is translation.updated's payload: which field of which record, in
// which language, what it is now, and who wrote it.
//
// SourceHash travels because it is the fact a reader cannot recompute without
// the source: a downstream index that serves translated text decides from it
// whether the copy it holds still matches what it was translated from.
// RequestID and Traceparent are not here because kit/events carries them on the
// envelope and the audit trail joins them to this row; a payload that repeated
// them would be two places for one trace to disagree with itself.
type Updated struct {
	Module     string     `json:"module"`
	Entity     string     `json:"entity"`
	RecordID   uuid.UUID  `json:"recordId" format:"uuid"`
	Field      string     `json:"field"`
	Locale     string     `json:"locale"`
	Value      string     `json:"value,omitempty"`
	Status     string     `json:"status"`
	Origin     string     `json:"origin"`
	SourceHash string     `json:"sourceHash,omitempty"`
	Revision   int64      `json:"revision"`
	Translator uuid.UUID  `json:"translator,omitempty" format:"uuid"`
	ReviewedAt *time.Time `json:"reviewedAt,omitempty"`
}

// Events is every event this module emits, for the manifest. kit/app refuses a
// route that would publish a name outside this list, so a command that gained a
// second event without declaring it stops the installation from starting.
var Events = []events.Declared{
	events.Declare[*Updated](EventUpdated),
}
