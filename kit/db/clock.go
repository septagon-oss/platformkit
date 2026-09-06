package db

import (
	"time"

	"github.com/google/uuid"
)

// NewID is the id every row and every event is born with: a version 7 UUID,
// which sorts by the moment it was made and, within one moment, by the order it
// was made in. Ordering by (created_at, id) is therefore creation order even for
// rows one transaction stamped with the same instant; a random id would leave
// that to chance.
func NewID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

// Now is the clock everything that writes a timestamp reads: UTC, truncated to
// the microsecond.
//
// Both halves are about the database. UTC, because a time that depends on where
// the process runs cannot be compared across two replicas in two regions, and
// timestamptz stores an instant rather than the offset it was written with, so
// the zone is lost anyway. Truncated to the microsecond, because that is the
// resolution timestamptz keeps: a Go time carries nanoseconds, so a value
// written and read back differs from the one in memory in its last three
// digits, and every equality on it — a test comparing what a command returned
// with what the next command read, an idempotency check that asks whether the
// resolution time moved — is a coin toss. Truncating at the source makes the
// round trip exact.
func Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }
