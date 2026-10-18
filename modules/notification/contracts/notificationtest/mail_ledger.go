package notificationtest

import (
	"context"
	"sync"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// FakeMailLedger is contracts.MailLedger over a slice: the same rules, no
// database, no transaction. It runs the same contracts.PrepareMail the SQL
// service runs, so a caller that writes a record the table would refuse fails
// here too rather than only against Postgres.
//
// What it cannot share with the SQL service is commit topology. "The row is
// written in the caller's transaction and disappears with it" is a Postgres fact,
// and a fake that claimed it would be a fake that lies about the one thing this
// ledger is for.
type FakeMailLedger struct {
	mu   sync.Mutex
	rows []contracts.MailRecord
}

// NewFakeMailLedger returns an empty ledger: no mail has left.
func NewFakeMailLedger() *FakeMailLedger { return new(FakeMailLedger) }

var _ contracts.MailLedger = (*FakeMailLedger)(nil)

// RecordMail mirrors internal.Service.RecordMail, refusals included, and appends
// in the order it was called, which is the order the SQL rows come back in.
func (l *FakeMailLedger) RecordMail(_ context.Context, _ db.Tx[db.Tenant], r contracts.MailRecord) error {
	prepared, err := contracts.PrepareMail(r)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rows = append(l.rows, prepared)
	return nil
}

// MailOutcome mirrors internal.Service.MailOutcome: the newest record for one
// request id, and known=false for a request that left none — including the empty
// id, which is refused rather than answered with somebody else's row. The kinds it
// is handed are the kinds it may answer from; with none it answers about every kind.
func (l *FakeMailLedger) MailOutcome(_ context.Context, _ db.Tx[db.Tenant], requestID string, kinds ...string) (string, bool, error) {
	if requestID == "" {
		return "", false, contracts.ErrMailRequest
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.rows) - 1; i >= 0; i-- {
		row := l.rows[i]
		if row.RequestID != requestID || !among(kinds, row.Kind) {
			continue
		}
		return row.Outcome, true, nil
	}
	return "", false, nil
}

// among is the kind filter with the same empty-means-everything rule as the SQL
// read, so a fake that answered a narrowed read from a kind the caller never named
// would fail the conformance case rather than pass a door that trusts it.
func among(kinds []string, kind string) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Rows is every record, in the order they were appended — what a consumer's own
// test asserts about the mails it caused.
func (l *FakeMailLedger) Rows() []contracts.MailRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]contracts.MailRecord, len(l.rows))
	copy(out, l.rows)
	return out
}
