package httpx

// versions.go is the port a mounted resource keeps its committed writes in.
//
// It is declared here, and not in kit/rest, because it has two readers that may
// not import each other: kit/rest, which records a version inside the write that
// made it, and ui/screens, which lists them on the record's own detail view.
// kit/rest already imports this package and ui/screens may not import kit/rest,
// so this is the one place both can reach. The shape is the one kit/rest already
// uses for the same kind of arrangement — a port declared by the code that needs
// it, satisfied by a module at composition, called inside the caller's
// transaction — which is rest.FileUses, and the reason is the same: a version
// recorded after the commit is a version that does not exist for as long as the
// write it describes is invisible.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// Versions is the store a resource with History keeps its versions in. Every
// method runs inside the caller's transaction, so a version is committed exactly
// when the write it describes is, and a refused write leaves no version behind.
//
// A version is identified by (module, entity, record, locale, version); the
// tenant is never an argument because it is the transaction's, which is what
// makes the isolation the database's rather than a caller's.
type Versions interface {
	// RecordVersion writes the version one committed write produced and answers
	// the version row's own id, which is what the images that body shows are
	// filed under. It refuses, writing nothing, when that number is already held
	// in this series or the document cannot be stored.
	RecordVersion(ctx context.Context, tx db.Tx[db.Tenant], in VersionInput) (uuid.UUID, error)
	// ListVersions answers a series newest first, at most Limit of them, starting
	// below Before when Before is above zero.
	ListVersions(ctx context.Context, tx db.Tx[db.Tenant], q VersionQuery) ([]Version, error)
	// Version answers one version of one record, and crud-style ErrNotFound when
	// this tenant holds no such version.
	Version(ctx context.Context, tx db.Tx[db.Tenant], key VersionKey) (Version, error)
}

// VersionInput is one write, as the door that made it saw it: which record, at
// which number, in which language, with what document and which field names the
// write moved. Reason is the write's own sentence about why it happened — empty
// for an ordinary save — and the grammar of it belongs to the store and its
// panel, never to a person's reading: the words a person reads come from the
// catalogue.
type VersionInput struct {
	Module, Entity, Locale string
	Record                 uuid.UUID
	Version                int64
	Fields                 json.RawMessage
	Changed                []string
	Reason                 string
	// Refs are the files the stored bodies show, per richtext field, as the door
	// that parsed them read them. The store files them as this version's uses and
	// keeps the list, so the versions it removes release exactly what the versions
	// it keeps hold alive.
	Refs []VersionRefs
	// At and Actor come from the write: the instant it committed, taken from the
	// row, which is what a person reads in the panel and what retention ages from;
	// and who made it, uuid.Nil when nobody signed in.
	At    time.Time
	Actor uuid.UUID
}

// VersionRefs are the files one richtext field of a stored body shows. The store
// files them; it does not resolve them.
type VersionRefs struct {
	Field string
	Files []uuid.UUID
}

// VersionKey names one version of one record.
type VersionKey struct {
	Module, Entity, Locale string
	Record                 uuid.UUID
	Version                int64
}

// VersionQuery names one series: one record's versions in one language.
type VersionQuery struct {
	Module, Entity, Locale string
	Record                 uuid.UUID
	Limit                  int
	// Before, above zero, asks for the page below that version — the "earlier"
	// page a panel that has already shown the newest twenty asks for.
	Before int64
}

// Version is one stored write. Fields is the document as that write committed it,
// after normalisation and validation, and it is the half a restore writes back.
// Actor is uuid.Nil when nobody signed in — a seed, a job, a system write — which
// is an answer and not an absent row.
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
