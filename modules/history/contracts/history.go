// Package contracts is the history module's whole outside: the versions a record
// has, the rule that decides how many of them stand, and the one service that
// writes and reads them.
//
// Nothing here is about any particular record: a version names the module, the
// entity and the record it describes, in one language, at one number, and the
// document is whatever that record's write committed. That is the whole vocabulary,
// which is why one implementation serves pages, prices and anything a client
// invents next (decision 0064's sixth line: the kernel holds the generic, the
// product holds the specific).
//
// A version is written by the door that wrote the row — kit/rest, in the write's
// own transaction — and so is never a copy of what a caller asked for. It is read
// by the record's own screen and by a restore. Nothing here rewrites or deletes a
// version: a restore is a new version with old content, and the only removal is
// the retention rule below, applied by a job.
package contracts

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// RestoredEvent and Restored are the write this module supports — a version
// brought back — spelled where they are published from. The kit owns the sentence
// because it performs the restore for whichever resource asked for history; this
// module owns the declaration, which is what makes a composition that mounts a
// resource with history and no history module a boot refusal rather than an event
// nobody answers.
var Events = []events.Declared{events.Declare[rest.Restored](rest.RestoredEvent)}

// Key is a version: one record's content, in one language, at one number. The
// tenant is not part of it because it is never an argument — it is the
// transaction's, which is what makes the isolation the database's rather than a
// caller's.
type Key struct {
	Module, Entity, Locale string
	Record                 uuid.UUID
	Version                int64
}

// Ref is one file a stored body shows, under the field that shows it.
type Ref struct {
	Field string
	Files []uuid.UUID
}

// Input is one write, as the door that made it saw it: which record, at which
// number, in which language, with what document and which field names the write
// moved. Reason is the write's own sentence about why it happened, empty for an
// ordinary save.
type Input struct {
	Module, Entity, Locale string
	Record                 uuid.UUID
	Version                int64
	Fields                 json.RawMessage
	Changed                []string
	Reason                 string
	Refs                   []Ref
	// At and Actor come from the write: the instant it committed and whoever made
	// it, uuid.Nil when nobody signed in.
	At    time.Time
	Actor uuid.UUID
}

// Query names one series: one record's versions in one language. Before, above
// zero, asks for the page older than that version.
type Query struct {
	Module, Entity, Locale string
	Record                 uuid.UUID
	Limit                  int
	Before                 int64
}

// ErrDuplicateNumber is the refusal one version cannot share with another: the
// series already holds that number, so the write that asked for it is a defect
// somewhere above this module rather than a condition a caller can act on. It is
// named here because the database's unique violation is the same fact, and a
// caller that has to recognise one has to recognise the other by the same name.
var ErrDuplicateNumber = fmt.Errorf("history: this version number is already taken")

// Service is the version store. Every method runs in the caller's transaction, so
// a version is committed exactly when the write it describes is, and a refused
// write leaves no version behind.
type Service interface {
	// Record writes the version one committed write produced and answers the
	// version row's id. It refuses, having written nothing, when the number is
	// already held in this series or the document names a file this tenant does
	// not have.
	Record(ctx context.Context, tx db.Tx[db.Tenant], in Input) (uuid.UUID, error)
	// Page answers a series newest first, at most Limit of them.
	Page(ctx context.Context, tx db.Tx[db.Tenant], q Query) ([]Version, error)
	// Version answers one version, and crud.ErrNotFound when this tenant holds no
	// such version.
	Version(ctx context.Context, tx db.Tx[db.Tenant], key Key) (Version, error)
	// Prune removes the versions this tenant's rule no longer keeps and releases
	// the files they were keeping alive. now is the clock the rule ages from,
	// passed in rather than read here so that a test fixes the same instant the
	// rule did. It answers how many versions went.
	Prune(ctx context.Context, tx db.Tx[db.Tenant], rule Rule, now time.Time) (int, error)
}

// Version is one stored write. Fields is the record as that write committed it,
// after its validation and its normalisation, and it is the half a restore writes
// back. Changed names the fields the write moved; the first version of a record
// carries none, because nothing preceded it to move. Actor is uuid.Nil when nobody
// signed in.
type Version struct {
	ID      uuid.UUID
	Module  string
	Entity  string
	Record  uuid.UUID
	Locale  string
	Version int64
	Fields  json.RawMessage
	Changed []string
	Reason  string
	Actor   uuid.UUID
	At      time.Time
}

// The two defaults, in the words the panel reads out loud: ninety days, or the
// newest fifty versions, whichever keeps more. Ninety days is long enough that a
// person who wonders about last month's edit still finds it, and short enough that
// a document's whole history is not kept forever; fifty is the ceiling for a record
// edited every day of those ninety, and the one that decides for a busy record.
const (
	DefaultDays     = 90
	DefaultVersions = 50
)

// Rule is how much history keeps. Days and Versions are both floors: a version is
// kept when either one of them says so, so the rule "keeps whichever keeps more",
// and a version older than Days is removed only once more than Versions stand above
// it. Zero on either side is the default above, and the pair is never a limit a
// person can mistake for the other.
type Rule struct {
	Days     int
	Versions int
}

// Defaults is the rule the module runs when its composition says nothing.
func Defaults() Rule { return Rule{Days: DefaultDays, Versions: DefaultVersions} }

// Fault names the rule that cannot be run: a period of nothing, or a ceiling of
// nothing, would sweep a tenant's whole history on its first hour. A rule that
// keeps nothing is not a retention policy, and refusing it is the only answer that
// leaves the versions standing.
func (r Rule) Fault() string {
	days, versions := r.orDefaults()
	if days < 1 || versions < 1 {
		return fmt.Sprintf("history retention of %d days and %d versions keeps nothing that can be restored", days, versions)
	}
	return ""
}

// Keep reports whether the version at this rank — 1 is the newest in its series —
// and this instant still stands. It is the whole retention decision, in one place,
// asked the same way by the SQL sweep and by anything else that has to answer what
// the rule holds: age and rank, never a third thing.
func (r Rule) Keep(rank int, committedAt, now time.Time) bool {
	days, versions := r.orDefaults()
	if rank <= versions {
		return true
	}
	return committedAt.After(now.Add(-time.Duration(days) * 24 * time.Hour))
}

func (r Rule) orDefaults() (days, versions int) {
	days, versions = r.Days, r.Versions
	if days <= 0 {
		days = DefaultDays
	}
	if versions <= 0 {
		versions = DefaultVersions
	}
	return days, versions
}
