package contracts

import (
	"time"

	"github.com/google/uuid"
)

// EventErased is the fact that this tenant stopped holding one person's data.
//
// It is the only thing the erasure publishes, and publishing it is not a
// courtesy: an erasure is irreversible, so the record that it happened has to
// commit in the same transaction as the rows it changed, and modules/audit
// subscribes to every event precisely so that this one reaches the trail
// (ADR 0020 §7). A command that scrubbed the rows and published nothing would
// leave a tenant unable to answer the one question an irreversible operation
// always raises afterwards.
const EventErased = "subject.erased"

// Erased is EventErased's payload. It names the person by id and never by the
// strings the erasure just removed: an event carrying them would copy a person's
// address into the outbox, the trail and every subscriber's store, which is the
// opposite of the command that emitted it.
type Erased struct {
	Subject uuid.UUID `json:"subject"`
	Modules []string  `json:"modules"`
	// Requester is whoever's request this was, read off the context the command
	// ran under, and the nil uuid when no request did — a job, a migration, an
	// operator at a terminal. It is the actor column of the trail row, which is
	// how "who erased them" stays answerable after the person holding that answer
	// is gone.
	Requester uuid.UUID `json:"requester"`
	At        time.Time `json:"at"`
}
