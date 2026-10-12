package notificationtest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
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
		"telling somebody writes a row and says so": func(t *testing.T, f Fixture) {
			got, err := f.Service.Notify(f.Ctx, f.Tx, notice("  A task was assigned to you  "))
			if err != nil {
				t.Fatalf("Notify: %v", err)
			}
			switch {
			case got.RecipientID != Ada:
				t.Errorf("the notice went to %s, want %s", got.RecipientID, Ada)
			case got.Title != "A task was assigned to you":
				t.Errorf("title is %q, want it trimmed", got.Title)
			case got.ReadAt != nil:
				t.Error("a new notification is already read")
			}
			published(t, f, contracts.EventCreated)
		},

		"a notice needs somebody to tell and something to say": func(t *testing.T, f Fixture) {
			for what, n := range map[string]contracts.Notice{
				"nobody":           {Title: "hello"},
				"nothing":          {Recipient: Ada},
				"an absolute link": {Recipient: Ada, Title: "hello", Link: "https://elsewhere.example.com"},
			} {
				if _, err := f.Service.Notify(f.Ctx, f.Tx, n); !errors.Is(err, crud.ErrInvalid) {
					t.Errorf("Notify with %s = %v, want ErrInvalid", what, err)
				}
			}
		},

		"asking for mail asks the worker for it": func(t *testing.T, f Fixture) {
			n := notice("A task was assigned to you")
			n.Email = true
			if _, err := f.Service.Notify(f.Ctx, f.Tx, n); err != nil {
				t.Fatalf("Notify: %v", err)
			}
			// Two events and no mail server: the send happens in the worker,
			// so a request never waits on somebody else's machine.
			published(t, f, contracts.EventCreated, contracts.EventEmailRequested)
		},

		"a recipient with no address gets the row and no mail": func(t *testing.T, f Fixture) {
			n := notice("A task was assigned to you")
			n.Recipient, n.Email = Bob, true
			got, err := f.Service.Notify(f.Ctx, f.Tx, n)
			if err != nil {
				t.Fatalf("Notify: %v", err)
			}
			if got.RecipientID != Bob {
				t.Errorf("the notice went to %s, want %s", got.RecipientID, Bob)
			}
			// Refusing the whole call would mean somebody with no address
			// could not be told anything.
			published(t, f, contracts.EventCreated)
		},

		"marking one read says so once": func(t *testing.T, f Fixture) {
			got, err := f.Service.Notify(f.Ctx, f.Tx, notice("A task was assigned to you"))
			if err != nil {
				t.Fatalf("Notify: %v", err)
			}
			read, err := f.Service.MarkRead(f.Ctx, f.Tx, got.ID, Ada)
			if err != nil {
				t.Fatalf("MarkRead: %v", err)
			}
			if read.ReadAt == nil {
				t.Error("a notification marked read has no time on it")
			}
			f.silent(t, "MarkRead again", func() {
				if _, err := f.Service.MarkRead(f.Ctx, f.Tx, got.ID, Ada); err != nil {
					t.Fatalf("MarkRead again: %v", err)
				}
			})
			published(t, f, contracts.EventCreated, contracts.EventRead)
		},

		"somebody else's notification is not found": func(t *testing.T, f Fixture) {
			got, err := f.Service.Notify(f.Ctx, f.Tx, notice("A task was assigned to you"))
			if err != nil {
				t.Fatalf("Notify: %v", err)
			}
			// Not a 403: the only thing a caller may learn about a
			// notification that is not theirs is that they do not have one
			// with that id.
			if _, err := f.Service.MarkRead(f.Ctx, f.Tx, got.ID, Bob); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("MarkRead of somebody else's notification = %v, want ErrNotFound", err)
			}
			if _, err := f.Service.MarkRead(f.Ctx, f.Tx, uuid.New(), Ada); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("MarkRead of an unknown id = %v, want ErrNotFound", err)
			}
		},

		"a query cannot re-address a list": func(t *testing.T, f Fixture) {
			// The recipient is the caller, resolved from the principal, and it
			// is not a parameter. Both implementations set the filter rather
			// than merging it, so there is no shape of Query that lists
			// somebody else's rows — which is what lets the route be SignedIn
			// with no permission at all. An implementation that honoured the
			// caller's filter would turn one signed-in person into a reader of
			// everybody's notifications, and it would look like paging.
			if _, err := f.Service.Notify(f.Ctx, f.Tx, notice("Ada's")); err != nil {
				t.Fatalf("Notify Ada: %v", err)
			}
			hers := notice("Bob's")
			hers.Recipient = Bob
			if _, err := f.Service.Notify(f.Ctx, f.Tx, hers); err != nil {
				t.Fatalf("Notify Bob: %v", err)
			}
			rows, total, err := f.Service.ListFor(f.Ctx, f.Tx, Bob, crud.Query{
				Filter: map[string]any{"recipientId": Ada},
			})
			if err != nil {
				t.Fatalf("ListFor with a filter of the caller's own: %v", err)
			}
			if total != 1 || len(rows) != 1 || rows[0].RecipientID != Bob {
				t.Fatalf("asking for Ada's rows as Bob returned %d of %d; the list is the caller's", len(rows), total)
			}
		},

		"a list is one person's, newest first": func(t *testing.T, f Fixture) {
			for _, title := range []string{"first", "second"} {
				if _, err := f.Service.Notify(f.Ctx, f.Tx, notice(title)); err != nil {
					t.Fatalf("Notify %s: %v", title, err)
				}
			}
			other := notice("not yours")
			other.Recipient = Bob
			if _, err := f.Service.Notify(f.Ctx, f.Tx, other); err != nil {
				t.Fatalf("Notify Bob: %v", err)
			}

			rows, total, err := f.Service.ListFor(f.Ctx, f.Tx, Ada, crud.Query{})
			if err != nil {
				t.Fatalf("ListFor: %v", err)
			}
			if total != 2 || len(rows) != 2 {
				t.Fatalf("Ada's list holds %d of %d rows, want her own two", len(rows), total)
			}
			if rows[0].Title != "second" {
				t.Errorf("the list starts with %q, want the newest first", rows[0].Title)
			}
			for _, row := range rows {
				if row.RecipientID != Ada {
					t.Errorf("Ada's list holds a row addressed to %s", row.RecipientID)
				}
			}
			if rows, _, err = f.Service.ListFor(f.Ctx, f.Tx, Ada, crud.Query{Limit: 1, Offset: 1}); err != nil ||
				len(rows) != 1 || rows[0].Title != "first" {
				t.Errorf("the second page of Ada's list = %v, %v", rows, err)
			}
		},
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
		"a sent mail is recorded with nothing to say": func(t *testing.T, f MailFixture) {
			request := mailRequest()
			// A mail the transport took has nothing to say about why: the reason
			// column is empty and the row is still the record of a send.
			f.record(t, contracts.MailRecord{
				Kind: "auth.set_password", Recipient: AdaMail,
				Outcome: contracts.MailSent, Reason: "", RequestID: request,
			})
			if got, known := f.answer(t, request); got != contracts.MailSent || !known {
				t.Errorf("recorded send answered %q known=%t, want sent true", got, known)
			}
		},

		"an outcome that is not sent says why": func(t *testing.T, f MailFixture) {
			f.refuse(t, contracts.MailRecord{
				Kind: "auth.set_password", Recipient: AdaMail,
				Outcome: contracts.MailFailed, RequestID: mailRequest(),
			}, contracts.ErrMailReason)
			// A request that mailed nothing is not refused and not answered: it is
			// the ordinary case, and the caller has to be able to tell it from a
			// record that says failed. The empty id is the same shape of mistake as
			// a record with no reason, so it is refused rather than answered with
			// the newest untraced row in the tenant.
			if got, known := f.answer(t, mailRequest()); got != "" || known {
				t.Errorf("a request that mailed nothing answered %q known=%t, want the empty answer", got, known)
			}
			err := f.Step(f.Ctx, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, known, err := f.Ledger.MailOutcome(ctx, tx, "")
				if known {
					t.Error("the empty request id was answered, which makes it somebody else's call")
				}
				return err
			})
			if !errors.Is(err, contracts.ErrMailRequest) {
				t.Errorf("MailOutcome(\"\") = %v, want the caller's mistake refused", err)
			}
		},

		"a kind is a name, not a sentence": func(t *testing.T, f MailFixture) {
			for _, kind := range []string{"", "Set Password", "auth", "auth.Set", "auth.set password"} {
				f.refuse(t, contracts.MailRecord{
					Kind: kind, Recipient: AdaMail, Outcome: contracts.MailSent, RequestID: mailRequest(),
				}, contracts.ErrMailKind)
			}
			// The checker is the table's own grammar, so the two names in use today
			// have to arrive, and the caller's own kind has to be one of them.
			for _, kind := range []string{"auth.set_password", "auth.verification"} {
				request := mailRequest()
				f.record(t, sentRecord(kind, request))
				if got, known := f.answer(t, request); got != contracts.MailSent || !known {
					t.Errorf("kind %q: answered %q known=%t, want sent true", kind, got, known)
				}
			}
		},

		"the read answers among the kinds it was given": func(t *testing.T, f MailFixture) {
			// One call, two mails: a door that speaks about one of them has to be
			// able to ask about it alone, and a read that answered it from whichever
			// row happened to be newest would answer about a mail the caller cannot
			// name (contracts.MailReport). The two rows share a request id on purpose:
			// it is the shape of one call that caused two sends somewhere else.
			request := mailRequest()
			f.record(t, sentRecord("auth.verification", request))
			f.record(t, contracts.MailRecord{
				Kind: "auth.set_password", Recipient: AdaMail, Outcome: contracts.MailFailed,
				Reason: "the relay refused", RequestID: request,
			})
			if got, known := f.answer(t, request); got != contracts.MailFailed || !known {
				t.Errorf("asked among every kind: %q known=%t, want the newest row, failed true", got, known)
			}
			if got, known := f.answer(t, request, "auth.verification"); got != contracts.MailSent || !known {
				t.Errorf("asked among auth.verification: %q known=%t, want sent true across the refused row", got, known)
			}
			if got, known := f.answer(t, request, "auth.set_password"); got != contracts.MailFailed || !known {
				t.Errorf("asked among auth.set_password: %q known=%t, want failed true", got, known)
			}
			// A kind the caller does not name is a row this read cannot see, and it
			// is answered exactly as a call that mailed nothing at all is.
			if got, known := f.answer(t, request, "auth.something_else"); got != "" || known {
				t.Errorf("asked among a kind with no row: %q known=%t, want the empty answer", got, known)
			}
		},

		"an outcome the table would refuse is refused here": func(t *testing.T, f MailFixture) {
			for _, outcome := range []string{"requested", "pending", ""} {
				// `requested` is the one that matters: it is legal in 000027 and
				// illegal here, because a mail with no notice has no first half to
				// be requested about. A caller that reached for the delivery
				// ledger's vocabulary is refused at the port, not by a CHECK.
				f.refuse(t, contracts.MailRecord{
					Kind: "auth.set_password", Recipient: AdaMail,
					Outcome: outcome, Reason: "the relay refused", RequestID: mailRequest(),
				}, contracts.ErrMailOutcome)
			}
		},

		"a reason that quotes the secret is written without it": func(t *testing.T, f MailFixture) {
			const token = "Zm9vYmFyYmF6AQT3yZ0pQ0d0dGhlIHF1aWNrIGJyb3duIGZveA"
			request := mailRequest()
			// The mailer that refuses by quoting its own input back is the reason a
			// reason has to be scrubbed on its way in (modules/auth/internal/
			// verification.go says the same thing about its error).
			said := "transport echoed its input: https://acme.example.com/auth/reset?token=" + token
			f.record(t, contracts.MailRecord{
				Kind: "auth.set_password", Recipient: AdaMail, Outcome: contracts.MailFailed,
				Reason: contracts.RedactMailReason(said, token, "sha256-of-"+token), RequestID: request,
			})
			// Over the bound is clipped, not refused: a long sentence that has lost
			// its secret is still worth recording.
			long := mailRequest()
			f.record(t, contracts.MailRecord{
				Kind: "auth.set_password", Recipient: AdaMail, Outcome: contracts.MailFailed,
				Reason: strings.Repeat("refused ", contracts.MaxMailReason), RequestID: long,
			})
			var reasons []string
			for _, row := range f.rows(t) {
				if row.RequestID == request || row.RequestID == long {
					reasons = append(reasons, row.Reason)
				}
			}
			if len(reasons) != 2 {
				t.Fatalf("the two refused mails left %d records, want 2", len(reasons))
			}
			for _, reason := range reasons {
				if strings.Contains(reason, token) {
					t.Errorf("a recorded reason carries the credential: %q", reason)
				}
				if len(reason) > contracts.MaxMailReason {
					t.Errorf("a recorded reason is %d bytes, over the bound of %d", len(reason), contracts.MaxMailReason)
				}
				if !utf8.ValidString(reason) {
					t.Errorf("a recorded reason ends mid-rune: %q", reason)
				}
			}
			if got, known := f.answer(t, request); got != contracts.MailFailed || !known {
				t.Errorf("a refused mail answered %q known=%t, want failed true", got, known)
			}
		},

		"one send is one row, in the order they happened": func(t *testing.T, f MailFixture) {
			requests := []string{mailRequest(), mailRequest(), mailRequest()}
			outcomes := []string{contracts.MailSent, contracts.MailFailed, contracts.MailSuppressed}
			for i, outcome := range outcomes {
				f.record(t, contracts.MailRecord{
					Kind: "auth.verification", Recipient: AdaMail, Outcome: outcome,
					Reason: map[string]string{contracts.MailSent: "", contracts.MailFailed: "the relay refused",
						contracts.MailSuppressed: "no mailer wired"}[outcome],
					RequestID: requests[i],
				})
			}
			var got []string
			for _, row := range f.rows(t) {
				if i := slices.Index(requests, row.RequestID); i >= 0 {
					got = append(got, row.Outcome)
				}
			}
			if !slices.Equal(got, outcomes) {
				t.Errorf("this case's records came back %v in ledger order, want %v", got, outcomes)
			}
			// Three sends are three rows: nothing here updates or deletes, so the
			// second attempt at one address does not overwrite the first.
			before := len(f.rows(t))
			f.record(t, contracts.MailRecord{
				Kind: "auth.verification", Recipient: AdaMail, Outcome: contracts.MailFailed,
				Reason: "the relay refused again", RequestID: requests[0],
			})
			if after := len(f.rows(t)); after != before+1 {
				t.Errorf("a second send for one request moved the count %d -> %d; a record is appended, never updated",
					before, after)
			}
			// And the answer for a request with two attempts is the last of them.
			if got, known := f.answer(t, requests[0]); got != contracts.MailFailed || !known {
				t.Errorf("newest of two=%q known=%t, want failed true", got, known)
			}
		},

		"a recipient is an address or nothing": func(t *testing.T, f MailFixture) {
			f.refuse(t, contracts.MailRecord{
				Kind: "auth.set_password", Outcome: contracts.MailSent, RequestID: mailRequest(),
			}, contracts.ErrMailRecipient)
			f.refuse(t, contracts.MailRecord{
				Kind: "auth.set_password", Recipient: strings.Repeat("a", 321),
				Outcome: contracts.MailSent, RequestID: mailRequest(),
			}, contracts.ErrMailRecipient)
			// An address is spelled by contracts.EmailKey at the call site and is
			// not re-normalised here — that would be the same fact normalised twice,
			// in two ways that could disagree. Only the whitespace around it goes.
			request := mailRequest()
			f.record(t, contracts.MailRecord{
				Kind: "auth.set_password", Recipient: "  Ada@Example.COM  ",
				Outcome: contracts.MailSent, RequestID: request,
			})
			for _, row := range f.rows(t) {
				if row.RequestID == request && row.Recipient != "Ada@Example.COM" {
					t.Errorf("the ledger re-spelled the address it was handed as %q", row.Recipient)
				}
			}
		},
	}
}
