package internal

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// The dispatch row is this module's record that it decided to send a credential, written
// in the transaction that minted the link it names and marked with whatever the transport
// answered afterwards. Its subject is the window between those two commits: a process
// that stops inside it leaves a link nobody was sent, and without this row nothing in the
// database can say so.
//
// The row is written by every offer that reaches the point of mailing — the two offers of
// a credential nobody holds, in password.go and verification.go. Rotation is the one
// decided send with no row, and that is deliberate rather than unfinished: its mail leaves
// before its credential is written, so the transaction that has the transport's answer
// already is the one that writes sent_at, the notice and the ledger record, and a row
// written before the send could be undone only by the refusal that ends the credential the
// person is holding (see rotate).
//
// Nothing reads this table over the network. It has no port method, no route and no
// screen, because the public delivery door's whole defence is that a caller is answered
// from a record its own call caused, and "is this person's mail still unsent" keyed by a
// request id or an address is the enumeration oracle that defence exists to refuse (see
// no_link.go). An operator reads it in SQL, inside the tenant's own scope.
//
// # The states, and who may write them
//
// unsent is the state the mint's transaction writes, and no other writer ever does. sent
// and withdrawn are written by the hand-off's own transaction, beside the marks of the
// same send (see delivered and undelivered). unknown is written by the sweep alone, for a
// row still unsent past the grace below, and it is terminal in the strong sense: no code
// path may move it to sent, because nothing can prove a mail left, and a state that
// quietly became sent would be a claim about the world nothing earned.
//
// Every mark names this attempt's own row and insists on unsent, so the sweep and a slow
// hand-off cannot both settle one row: the later writer finds no row to settle and writes
// nothing. A mark that loses that race is only reachable if one hand-off outlived
// handoverBudget inside a grace four times its length, and its consequence is a mail that
// did leave being recorded unknown — the conservative direction, an honest "we do not
// know" rather than a false "we sent it".
//
// # What is kept, and for how long
//
// A settled row is forgotten 90 days after it settled. An unsettled one is never
// forgotten: it is the evidence of a lost send, and a sweep that deleted it would be the
// mechanism that hides the loss this table exists to make visible. The cost, said out
// loud, is that a host that crashes mid-send leaves rows nothing removes but an
// operator's hand or the person's own erasure — and their count is the metric.
//
// # What this does not promise
//
// It does not make a send survivable, and nothing can: the plaintext lived only in the
// memory that died, so the recovery is the person asking again, which no cooldown holds
// off because both cooldowns count sent_at and a never-sent row carries no mark. It does
// not make a send exactly-once either — a transport with no idempotency key can take a
// message and die before its marks commit, leaving a row that reads unsent while the mail
// is in the mailbox. The row's honesty is that it never claims more than the transport
// proved.
const (
	// dispatchUnsentGrace is how long a row may stand unsent before this module stops
	// claiming anybody remembers it. It is four times handoverBudget — the ceiling on one
	// hand-off's slot wait, send and marks — so no hand-off still inside its own budget
	// can be called lost; the comparison is the database's clock against itself, so no
	// deployment clock can make it bite early; and it is a quarter of the shortest resend
	// cooldown and a thirtieth of the shortest credential life, so no person's experience
	// turns on it.
	dispatchUnsentGrace = 2 * time.Minute
	// dispatchRetention is how long a settled row outlives the send it describes. Ninety
	// days is this module's own longest stated horizon — SessionMaxLifetime — and a
	// table's retention is its owner's promise rather than a shared default borrowed from
	// wherever somebody else needed the same number. The credential the row names expired
	// within a day of being written, so what is kept past that is an operator's reading
	// distance, not a live door.
	dispatchRetention = 90 * 24 * time.Hour
)

// The four states, as this module's SQL and its statements spell them. They stay in here
// because their only readers are in here: the two marks, the sweep and the INSERT. A
// module exports a name when something outside has to agree on it, and nothing outside
// this package decides what a dispatch row says.
const (
	dispatchUnsent    = "unsent"
	dispatchSent      = "sent"
	dispatchWithdrawn = "withdrawn"
	dispatchUnknown   = "unknown"
)

var (
	// The two cutoffs, as Postgres spells an interval, so the sweep's arithmetic happens on
	// the server that owns the rows rather than in whichever process noticed them.
	unsentGrace = fmt.Sprintf("%d minutes", int(dispatchUnsentGrace/time.Minute))
	settledFor  = fmt.Sprintf("%d days", int(dispatchRetention/24/time.Hour))
)

// writeMailDispatch records the message this transaction has decided to send, beside the
// credential row it mints and in the same transaction, so the evidence has exactly the
// lifetime of the credential it names: a rollback writes neither, and a mint that returned
// before it decided to mail writes neither.
//
// It takes everything from the hand-off's own description, which is built before the
// credential is written, so the row and the message are described from one set of values.
// Only the hash of the token is written; the token itself stays in the message and in no
// row anywhere.
//
// A row this module cannot describe fails the mint rather than being skipped: the pair has
// to be one fact. A credential committed under a dispatch row that could not be written
// would be a live link with no record of the mail it was supposed to arrive by.
func writeMailDispatch(tx db.Tx[db.Tenant], m linkMail) error {
	recipient := contracts.EmailKey(m.address)
	switch {
	case len(m.digest) == 0 || m.token == "":
		return fmt.Errorf("auth: describe the %s dispatch without its credential: no token to name the row", m.kind)
	case m.person == uuid.Nil:
		return fmt.Errorf("auth: describe a %s dispatch for nobody", m.kind)
	case recipient == "" || len(recipient) > 320:
		return fmt.Errorf("auth: describe a %s dispatch for an address that is not an address", m.kind)
	case m.kind != contracts.MailSetPassword && m.kind != contracts.MailVerification:
		return fmt.Errorf("auth: dispatch a mail of kind %q, which this module does not send", m.kind)
	}
	res := tx.DB().Exec("INSERT INTO auth_mail_dispatch (tenant_id, user_id, token_hash, kind, recipient, state)"+
		" VALUES (?, ?, ?, ?, ?, ?)",
		db.TenantOf(tx).ID, m.person, m.digest, m.kind, recipient, dispatchUnsent)
	if res.Error != nil {
		return fmt.Errorf("auth: record the %s mail this transaction decided: %w", m.kind, res.Error)
	}
	return nil
}

// markDispatch records the fate one credential message reached, inside the hand-off's own
// transaction so the state, the credential's sent_at and the ledger row are one commit
// rather than three facts that can disagree.
//
// The row is named by this attempt's own hash and only while it still reads unsent, so a
// row the sweep has already given up on keeps the state the sweep gave it: RowsAffected
// zero says somebody settled this first, and settling it twice would be rewriting a fate
// that was already recorded. A row that was never written — a mint from before this table
// existed, on an installation that is mid-upgrade — is not an error either, and no send is
// failed for the absence of its record.
func markDispatch(tx db.Tx[db.Tenant], m linkMail, state string) error {
	res := tx.DB().Exec("UPDATE auth_mail_dispatch SET state = ?, settled_at = clock_timestamp()"+
		" WHERE tenant_id = ? AND token_hash = ? AND state = ?",
		state, db.TenantOf(tx).ID, m.digest, dispatchUnsent)
	if res.Error != nil {
		return fmt.Errorf("auth: record that the %s mail %s: %w", m.kind, state, res.Error)
	}
	return nil
}

// loseUnsentDispatch gives up on the messages nobody came back to mark, and says so rather
// than pretending they were sent. This is the only writer of unknown, and the only writer
// of anything to a row that has outlived the memory that held its plaintext.
func loseUnsentDispatch(tx db.Tx[db.Tenant]) (int64, error) {
	res := tx.DB().Exec("UPDATE auth_mail_dispatch SET state = ?, settled_at = clock_timestamp()"+
		" WHERE (tenant_id, token_hash) IN (SELECT tenant_id, token_hash FROM auth_mail_dispatch"+
		" WHERE state = ? AND created_at < clock_timestamp() - ?::interval LIMIT ?)",
		dispatchUnknown, dispatchUnsent, unsentGrace, purgeBatch)
	if res.Error != nil {
		return 0, fmt.Errorf("auth: record the credential mails nobody came back to mark: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// forgetSettledDispatch drops the settled rows past this module's horizon. An unsettled row
// is never in its own result: the row nobody settled is the evidence of a lost send, and
// deleting it is the exact thing this table exists to prevent.
func forgetSettledDispatch(tx db.Tx[db.Tenant]) (int64, error) {
	res := tx.DB().Exec("DELETE FROM auth_mail_dispatch"+
		" WHERE (tenant_id, token_hash) IN (SELECT tenant_id, token_hash FROM auth_mail_dispatch"+
		" WHERE settled_at IS NOT NULL AND settled_at < clock_timestamp() - ?::interval LIMIT ?)",
		settledFor, purgeBatch)
	if res.Error != nil {
		return 0, fmt.Errorf("auth: forget the settled credential mails: %w", res.Error)
	}
	return res.RowsAffected, nil
}
