package notificationtest

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// Fixture is one case's world: a Service, the transaction its commands take,
// and a way to see what was published.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// Published is the names of the events published so far, in order. It is
	// what holds MarkRead to saying nothing the second time, which is the half
	// of the promise a return value cannot show.
	Published func() []string
}

// silent runs step and fails if it published anything.
func (f Fixture) silent(t *testing.T, what string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	if after := f.Published(); len(after) != before {
		t.Errorf("%s published %v; repeating a command changes nothing, so it says nothing", what, after[before:])
	}
}

// Harness builds one Fixture and calls run with it. It is written this way
// round because the real service's fixture is a transaction, and a transaction
// is a scope somebody has to close.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	for name, run := range cases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

// Ada is the recipient the suite writes to, and Bob is somebody else. They are
// exported because a harness has to tell the RecipientLookup which of them has
// an address: Ada does, Bob does not, which is what makes "a recipient with no
// address gets the row and no mail" a case.
var (
	Ada      = uuid.New()
	Bob      = uuid.New()
	AdaEmail = "ada@acme.example.com"
)

// notice is one notice for Ada.
func notice(title string) contracts.Notice {
	return contracts.Notice{Recipient: Ada, Title: title, Body: "the details", Link: "/admin/task/tasks/1"}
}

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"telling somebody writes a row and says so":                       writeNotification,
		"a notice needs somebody to tell and something to say":            validateNotice,
		"asking for mail asks the worker for it":                          requestMail,
		"the email flag a notice was written with asks the worker for it": deliverFlaggedMail,
		"a recipient with no address gets the row and no mail":            retainNoticeWithoutAddress,
		"marking one read says so once":                                   markReadOnce,
		"somebody else's notification is not found":                       concealAnotherRecipientsNotice,
		"a query cannot re-address a list":                                preserveListRecipient,
		"a list is one person's, newest first":                            listNewestFirst,
	}
}

func published(t *testing.T, f Fixture, want ...string) {
	t.Helper()
	if f.Published == nil {
		return
	}
	if got := f.Published(); !slices.Equal(got, want) {
		t.Errorf("published %v, want %v", got, want)
	}
}

// MailFixture is one case's world for the record of a mail that left with no
// notice behind it: the implementation under test, a way to run one step, and a
// way to read back what the implementation holds.
type MailFixture struct {
	Ctx    context.Context
	Ledger contracts.MailLedger
	// Step runs fn once and returns what it returned. One step is one
	// transaction: a refusal ends a Postgres transaction, so a suite that asked
	// for five refusals inside one would see the first and four aborted
	// statements, and would read the four as passes. The fake runs the step with
	// no transaction at all, which is the same promise seen from the side a fake
	// can reach — what it cannot share with the table is commit topology, and the
	// two cases that ask about it live in modules/notification/internal.
	Step func(ctx context.Context, fn func(context.Context, db.Tx[db.Tenant]) error) error
	// Rows is every record the implementation holds, oldest first: the table's
	// seq order and the fake's slice order, which are the same order.
	Rows func(t testing.TB) []contracts.MailRecord
}

// MailHarness builds one MailFixture and calls run with it, for the same reason
// Harness exists: the real ledger's fixture is a transaction, and a transaction
// is a scope somebody has to close.
type MailHarness func(t *testing.T, run func(MailFixture))

// RunMailLedger is the mail ledger's conformance suite: the rules every
// implementation of contracts.MailLedger applies, which are the CHECK clauses of
// migrations/000044 and no others. Both the SQL service and
// notificationtest.FakeMailLedger pass it; a fake that did not would be a
// quieter opinion about what a delivery record means.
func RunMailLedger(t *testing.T, h MailHarness) {
	t.Helper()
	for name, run := range mailCases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f MailFixture) { run(t, f) })
		})
	}
}

// AdaMail is the address the mail suite writes to — the same person the service
// suite tells, spelled the way contracts.EmailKey spells it at a call site.
const AdaMail = "ada@acme.example.com"

// mailRequest makes a request id for one case. Every case writes its own, so a
// case that ran after another reads its own rows and not the other's.
func mailRequest() string { return uuid.NewString() }

// record writes r and fails if the ledger refused a record it should take.
func (f MailFixture) record(t *testing.T, r contracts.MailRecord) {
	t.Helper()
	if err := f.Step(f.Ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return f.Ledger.RecordMail(ctx, tx, r)
	}); err != nil {
		t.Fatalf("RecordMail(%s to %s): %v", r.Outcome, r.Recipient, err)
	}
}

// refuse writes r and fails unless the ledger refused it with want.
func (f MailFixture) refuse(t *testing.T, r contracts.MailRecord, want error) {
	t.Helper()
	err := f.Step(f.Ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return f.Ledger.RecordMail(ctx, tx, r)
	})
	if !errors.Is(err, want) {
		t.Errorf("RecordMail(kind=%q outcome=%q) = %v, want %v", r.Kind, r.Outcome, err, want)
	}
}

// answer is what the ledger says about one request id, among the kinds it is given
// and among every kind when it is given none.
func (f MailFixture) answer(t *testing.T, request string, kinds ...string) (string, bool) {
	t.Helper()
	var (
		outcome string
		known   bool
	)
	err := f.Step(f.Ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		outcome, known, err = f.Ledger.MailOutcome(ctx, tx, request, kinds...)
		return err
	})
	if err != nil {
		t.Fatalf("MailOutcome(%s): %v", request, err)
	}
	return outcome, known
}

// rows reads back everything the ledger holds, and fails a case that cannot say.
func (f MailFixture) rows(t *testing.T) []contracts.MailRecord {
	t.Helper()
	if f.Rows == nil {
		t.Fatal("a mail ledger harness that cannot list its rows cannot run the cases that count them")
	}
	return f.Rows(t)
}

// sentRecord is a mail the transport took, which has no reason to give.
func sentRecord(kind, request string) contracts.MailRecord {
	return contracts.MailRecord{Kind: kind, Recipient: AdaMail, Outcome: contracts.MailSent, RequestID: request}
}

func mailCases() map[string]func(*testing.T, MailFixture) {
	return map[string]func(*testing.T, MailFixture){
		"a sent mail is recorded with nothing to say":           recordSentMail,
		"an outcome that is not sent says why":                  requireFailureReason,
		"a kind is a name, not a sentence":                      validateMailKind,
		"the read answers among the kinds it was given":         queryMailKinds,
		"an outcome the table would refuse is refused here":     validateMailOutcome,
		"a reason that quotes the secret is written without it": redactMailSecret,
		"one send is one row, in the order they happened":       preserveMailOrder,
		"a recipient is an address or nothing":                  validateMailRecipient,
	}
}
