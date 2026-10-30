package internal

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// The dispatch row is this module's record that it decided to send a credential, written in
// the transaction that minted the link it names and marked with whatever the transport
// answered afterwards. Its subject is the window between those two commits: a process that
// stops inside it leaves a link nobody was sent, and without this row nothing in the
// database can say so.

// Every offer that reaches the point of mailing writes one, for the two credentials nobody
// holds yet (password.go, verification.go). Rotation is the one decided send with no row,
// deliberately: its mail leaves before its credential is written, so the transaction that
// already has the transport's answer is the one writing sent_at, the notice and the ledger
// row, and a row written ahead of that send could be undone only by the refusal that ends
// the credential the person is holding (see rotate).

// Nothing reads this table over the network: no port method, no route, no screen, for the
// reason in this table's own SQL header. An operator reads it in SQL, in tenant scope.
//
// # The states, and who may write them
//
// unsent is written by the mint's transaction and by no other writer. sent and withdrawn are
// written by the hand-off's own transaction, beside the marks of the same send (see delivered
// and undelivered). unknown is written by the sweep alone, for a row still unsent past the
// grace below, and it is terminal: no path may move it to sent, because nothing can prove a
// mail left and a state that quietly became sent would be a claim nothing earned.

// Every mark names this attempt's own row and insists on unsent, so a slow hand-off and the
// sweep cannot both settle one row: the later writer finds nothing to settle. That race is
// only reachable if one hand-off outlived handoverBudget inside a grace four times its
// length, and it ends with a mail that did leave recorded unknown — the honest direction. The
// hand-off that loses it keeps its other marks back too, because they are marks of the same
// message and the row they belong to says this module cannot say (see markDispatch).
const (
	// dispatchUnsentGrace is how long a row may stand unsent before this module stops
	// claiming anybody remembers it. It is four times handoverBudget — the ceiling on one
	// hand-off's slot wait, send and marks — so no hand-off still inside its own budget
	// can be called lost; the comparison is the database's clock against itself, so no
	// deployment clock can make it bite early; and it is a quarter of the shortest resend
	// cooldown and a thirtieth of the shortest credential life, so no person's experience
	// turns on it.
	dispatchUnsentGrace = 2 * time.Minute
	// dispatchRetention is how long a settled row outlives the send it names; see
	// forgetSettledDispatch for why this horizon and why nothing past it.
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

// A mark says at most what the transport proved. This table makes no send survivable — the
// plaintext lived only in the memory that handed it over — and no send exactly-once, since the
// transport keeps no idempotency key to outlast: a hand-off that took a message and died before
// its marks commit leaves a row reading unsent beside a mail that did arrive. The recovery is
// the person asking, which no cooldown holds off.

// markDispatch records the fate one credential message reached, in the hand-off's own
// transaction, so its state, the credential's sent_at and the ledger row are one commit. It
// reports whether the marks of this send may stand beside that record: true when this write
// settled the row, and when the send named no row of its own, and false when the row is there
// and the sweep settled it first — the claim `sent` is one that record no longer supports, so
// the caller keeps every mark of the send back rather than this column alone (see delivered).
//
// The row is named by this attempt's own hash and only while it still reads unsent, and the
// settle and the reading of what that name holds are one statement, so no hand-off can read
// the sweep mid-write: the count says who won, and the existence says whether there was a row
// to win, which RowsAffected read alone cannot tell from a send that had none.
func markDispatch(tx db.Tx[db.Tenant], m linkMail, state string) (own bool, err error) {
	tenant := db.TenantOf(tx).ID
	var settled, named bool
	scan := tx.DB().Raw("WITH settled AS (UPDATE auth_mail_dispatch"+
		" SET state = ?, settled_at = clock_timestamp()"+
		" WHERE tenant_id = ? AND token_hash = ? AND state = ? RETURNING 1)"+
		" SELECT (SELECT count(*) FROM settled) > 0,"+
		" EXISTS (SELECT 1 FROM auth_mail_dispatch WHERE tenant_id = ? AND token_hash = ?)",
		state, tenant, m.digest, dispatchUnsent, tenant, m.digest).Row().Scan(&settled, &named)
	if scan != nil {
		return false, fmt.Errorf("auth: record that the %s mail %s: %w", m.kind, state, scan)
	}
	return settled || !named, nil
}

// errSendAlreadySettled is the hand-off's own rollback signal: its dispatch record was
// settled by the sweep before the transport answered, so the marks of this send describe a
// message this module no longer records and the whole of them come away unwritten. It never
// leaves delivered, which turns it into a logged decision and a nil answer (see delivered).
var errSendAlreadySettled = errors.New("auth: the record of this send was settled before its answer came back")

// settleDispatch is Purge's share of this table, in one call: give up on the messages
// nobody came back to mark, then forget the settled ones past the horizon. It returns what
// left the table and what changed, so the batch loop can tell a full batch from an empty one.
func settleDispatch(tx db.Tx[db.Tenant]) (int64, error) {
	lost, err := loseUnsentDispatch(tx)
	if err != nil {
		return 0, err
	}
	forget, err := forgetSettledDispatch(tx)
	if err != nil {
		return 0, err
	}
	return lost + forget, nil
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

// forgetSettledDispatch drops the settled rows past this module's horizon — its own horizon,
// SessionMaxLifetime, and not a number borrowed from wherever another table needed the same
// one. A credential expired within a day of being written, so what a settled row keeps past
// that is an operator's reading distance and not a live door.
//
// An unsettled row is never in its own result: it is the evidence of a lost send, and deleting
// it would hide the loss this table exists to make visible. The cost is that a host which
// crashes mid-send leaves rows nothing removes but an operator's hand or the person's own
// erasure — and their count is the metric.
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
