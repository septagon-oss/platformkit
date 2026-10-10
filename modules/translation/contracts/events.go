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

	// Changes is what this write replaced, in the member names above: value,
	// status and reviewedAt, each with the half it was and the half it became.
	// It is the emitting module's half of "every audited change carries what
	// changed" (kit/events/change.go), and the trail stores it verbatim, so a
	// reader of the history of one paragraph's Portuguese sees the paragraph
	// and not only the row that survived.
	//
	// hidden, and not readOnly: the sentence is kit/events/change.go's — no REST
	// response body holds a diff, because the door sets it beside the publish and
	// clears it after — and an event does carry it. The row itself is not what is
	// diffed here: revision, translator and sourceHash describe the write, and a
	// diff of them would be a fourth account of the same write.
	Changes []events.Change `json:"changes,omitempty" hidden:"true"`
}

// Events is every event this module emits, for the manifest. kit/app refuses a
// route that would publish a name outside this list, so a command that gained a
// second event without declaring it stops the installation from starting.
var Events = []events.Declared{
	events.Declare[*Updated](EventUpdated),
}
