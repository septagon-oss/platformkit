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

// RestoredEvent names the write that brought a stored version back. The contract
// lives beside the Versions port it describes, and for the same reason: the kit
// publishes it, for whichever resource asked for history, and the module that
// keeps the versions declares it typed in its manifest — a contracts/ package may
// name no SDK and no door, so this is the one place both readers can reach.
const RestoredEvent = "history.restored"

// Restored is RestoredEvent's payload: which record, which version came back,
// which number the write landed on, in which language, and when. From and To are
// the two numbers a person reads after a restore — “Restored from v3” and the
// version the record is on now — and the pair is why the payload is not simply
// the entity: the entity says what the row is, and only this says a version of it
// was brought back. The json names are the wire contract, spelled the way every
// other event payload this repository publishes spells itself.
type Restored struct {
	Module string    `json:"module" doc:"The module that owns the record"`
	Entity string    `json:"entity" doc:"The record's entity within that module"`
	Record uuid.UUID `json:"record" format:"uuid" doc:"The record a version of was written back"`
	From   int64     `json:"from" doc:"The version that came back"`
	To     int64     `json:"to" doc:"The number the write landed on"`
	Locale string    `json:"locale" doc:"The series it was read from, as a BCP-47 tag"`
	Actor  uuid.UUID `json:"actor" format:"uuid" doc:"Who asked for it; the zero uuid when nobody signed in"`
	At     time.Time `json:"at" doc:"When the write committed"`
}

// Version is one stored write. Fields is the document as that write committed it,
// after normalisation and validation, and it is the half a restore writes back.
// Actor is uuid.Nil when nobody signed in — a seed, a job, a system write — which
// is an answer and not an absent row.
//
// The json tags are the wire contract, not decoration: this struct is what a
// record's own address answers its version list with, beside the record body that
// spells itself id/createdAt/revision. A response that spelled its items with Go's
// exported names would publish a second contract next to the first, and break every
// client — the panel, the mobile counterpart, anything generated from the OpenAPI
// document — the day it was fixed. The names below are the names those readers use.
type Version struct {
	ID      uuid.UUID       `json:"id" format:"uuid" doc:"This version row's own id"`
	Module  string          `json:"module" doc:"The module that owns the record this version describes"`
	Entity  string          `json:"entity" doc:"The record's entity within that module"`
	Record  uuid.UUID       `json:"record" format:"uuid" doc:"The record this is one version of"`
	Locale  string          `json:"locale" doc:"The language of this version series, as a BCP-47 tag"`
	Version int64           `json:"version" doc:"This version's number in its series — the row's own write count when the write made it, from 1"`
	Fields  json.RawMessage `json:"fields" doc:"The record as this write committed it, in the field names the record's own body uses"`
	Changed []string        `json:"changed" doc:"The fields this write moved; empty on a record's first version, which nothing preceded"`
	Reason  string          `json:"reason" doc:"The write's own sentence about why it happened; empty for an ordinary save"`
	Actor   uuid.UUID       `json:"actor" format:"uuid" doc:"Who made this write; the zero uuid when nobody signed in"`
	At      time.Time       `json:"at" doc:"When this write committed"`
}
