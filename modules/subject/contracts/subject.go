// Package contracts is the data subject's own door: the two operations a
// controller owes a person, answered by one module that owns none of the rows.
//
// modules/subject composes the `personaldata.Part` every owner contributes and
// turns the eight answers into one export and one erasure. It is the only place
// in the application allowed to say "this person, everywhere", for the reason
// kit/personaldata gives: house rule 3 forbids any one owner reading another's
// tables, so no owner can answer for the whole person (ADR 0020 §1).
//
// What lives here is the shape of the answer and nothing else. The person's
// identifiers do not appear in it on purpose: an export names the person by id and
// the bundle carries the strings, because a contract that took an email address as
// an argument would be an API that logs one.
package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// Bundle is one person's export: the zip a person can open, and the manifest
// that says what is in it and what is not.
type Bundle struct {
	// Zip is the archive: `manifest.json` first and one `<module>.json` per
	// part, entries sorted by name with a zero modified time, so two exports of
	// the same rows are the same bytes and a bundle can be digested and compared.
	Zip []byte `json:"-"`
	// Digest is the sha256 of Zip in hex, and is what a support conversation
	// quotes. It is not a signature and claims nothing about who wrote it.
	Digest string `json:"digest"`
	// Manifest is the archive's own first entry, kept beside the bytes because a
	// caller that stores the bundle still has to answer "what did we send"
	// without unzipping it.
	Manifest Manifest `json:"manifest"`
}

// Manifest names the moment, the person and every module that was asked.
type Manifest struct {
	// Tenant is the tenant whose rows these are — read off the transaction,
	// never handed in by a caller, so a bundle cannot name a tenant it was not
	// read from.
	Tenant  uuid.UUID `json:"tenant"`
	Subject uuid.UUID `json:"subject"`
	At      time.Time `json:"at"`
	Parts   []Part    `json:"parts"`
	// LeftOut names what the bundle does not carry. A person asking "what is
	// missing" has to be answered from the bundle itself, which is why an absent
	// entry is a sentence and not a silence. The bytes of a tenant's object store
	// are the first thing this will hold.
	LeftOut []string `json:"leftOut,omitempty"`
}

// Part is one module's entry.
type Part struct {
	Module string `json:"module"`
	Bytes  int    `json:"bytes"`
	Digest string `json:"digest"`
}

// EraseInput is one request to stop holding a person's data.
type EraseInput struct {
	Subject uuid.UUID

	// Confirm is the person's own address, repeated by the one asking. It is not
	// a secret and it buys nothing: it is the keystroke that stops a mis-clicked
	// request from being the moment a tenant's record of a person disappears
	// forever (ADR 0020 §7, after tenant.Delete's Confirm).
	Confirm string
}

// Erasure is the receipt an erasure returns.
type Erasure struct {
	Subject uuid.UUID `json:"subject"`
	// Modules names every owner that was asked to erase, in the order it was
	// asked: the receipt has to say which tables a person's erasure reached.
	Modules []string `json:"modules"`
	// AlreadyErased says the person's own row carries the replacement this module
	// writes, so nothing was rewritten and nothing was published. An erasure is
	// irreversible, which is exactly why asking twice answers the same receipt
	// rather than failing the second time.
	AlreadyErased bool `json:"alreadyErased,omitempty"`
	// At is when the erasure committed, on the clock this service was built with.
	At time.Time `json:"at"`
}

// Service is the two operations. Both take the caller's authoritative
// transaction, so the erasure's writes, its re-read and its event commit together
// or not at all, and neither can be pointed at a tenant the transaction was not
// opened against.
type Service interface {
	// Export is a read: it publishes nothing, writes nothing, and answers with
	// one person's rows as the bundle ADR 0020 §5 describes. One contributor
	// failing fails the whole export, because a partial bundle that reads as a
	// whole one is the failure this design cannot afford.
	Export(ctx context.Context, tx db.Tx[db.Tenant], subject uuid.UUID) (*Bundle, error)

	// Door, the two doors as a caller reaches them: a request that commits, the work the
	// worker does later, and the single-use link onto what it produced.
	Door

	// Erase asks every owner for its own rows, in module-name order, and
	// publishes subject.erased in the same commit as the rows it changed.
	// A refused erasure — a person who is not here, an address that is not the one
	// their row holds, the last administrator — writes nothing, emits nothing and
	// returns no receipt.
	Erase(ctx context.Context, tx db.Tx[db.Tenant], in EraseInput) (*Erasure, error)
}
