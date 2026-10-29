package notificationtest

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// FakeMailIntents is contracts.MailIntents over a slice: the same refusals, the same
// one-intent-per-(purpose, cause) rule, no database. It runs the same
// contracts.PrepareIntent the SQL service runs, so a caller that hands in what 000076
// would refuse fails here too rather than only against Postgres.
//
// What it cannot share with the SQL service is commit topology. "The intent is
// written in the caller's transaction and disappears with it", "a committed intent
// survives a restart" and "an attempt whose answer never came is unknown" are
// Postgres facts, and a fake that claimed them would be a fake that lies about the
// one thing this port is for. Those cases live in the module's own suite.
type FakeMailIntents struct {
	mu    sync.Mutex
	rows  []contracts.MailIntent
	claim func(claims int)
}

// NewFakeMailIntents returns an empty owed list: nothing is owed to anybody.
func NewFakeMailIntents() *FakeMailIntents { return new(FakeMailIntents) }

var _ contracts.MailIntents = (*FakeMailIntents)(nil)

// Queue mirrors internal.Service.Queue, refusals included, and returns the row a
// repeat of a (purpose, cause) already owed rather than the caller's input re-read
// back at it. An intent written without a cause is its own row every time.
func (f *FakeMailIntents) Queue(_ context.Context, _ db.Tx[db.Tenant], mail contracts.OutgoingMail) (*contracts.MailIntent, bool, error) {
	prepared, err := contracts.PrepareIntent(mail)
	if err != nil {
		return nil, false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if prepared.Cause != "" {
		for i := range f.rows {
			if f.rows[i].Purpose == prepared.Purpose && f.rows[i].Cause == prepared.Cause {
				return &f.rows[i], false, nil
			}
		}
	}
	now := time.Now().UTC()
	f.rows = append(f.rows, contracts.MailIntent{
		ID: uuid.New(), Seq: int64(len(f.rows) + 1), Purpose: prepared.Purpose, Cause: prepared.Cause,
		Person: prepared.Person, Notice: prepared.Notice, Recipient: prepared.Message.To,
		Subject: prepared.Message.Subject, Body: prepared.Message.Body, HTML: prepared.Message.HTML,
		Lang: prepared.Message.Lang, ReplyTo: prepared.Message.ReplyTo,
		State: contracts.MailIntentQueued, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	})
	return &f.rows[len(f.rows)-1], true, nil
}

// Owed is what the fake holds, in the order it was written.
func (f *FakeMailIntents) Owed() []contracts.MailIntent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.rows)
}

// IntentFixture is one case's world: the port, and the tenant transaction its
// commands take. The transaction is part of the fixture because the port takes one:
// a fake ignores it and the Postgres service cannot work without it.
type IntentFixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Intents contracts.MailIntents
}

// IntentHarness builds one fixture and calls run with it, the way Harness does for
// the service suite: the real fixture is a transaction, and a transaction is a scope
// somebody has to close.
type IntentHarness func(t *testing.T, run func(IntentFixture))

// RunMailIntents is the suite every implementation of contracts.MailIntents passes,
// fake and Postgres alike. The cases here are the ones both can answer; the ones
// about which transaction wrote the row cannot be answered by a fake at all and
// belong to the module's own suite over a real database.
func RunMailIntents(t *testing.T, h IntentHarness) {
	t.Helper()
	for name, run := range intentCases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f IntentFixture) { run(t, f) })
		})
	}
}

func intentCases() map[string]func(*testing.T, IntentFixture) {
	return map[string]func(*testing.T, IntentFixture){
		"a mail is owed once per cause and purpose":    queueOncePerCause,
		"a mail that names no cause is owed twice":     queueWithoutCause,
		"a purpose is a name and not a sentence":       purposeIsAName,
		"a mail names the address it is owed to":       recipientIsRequired,
		"a mail has a text body and a size":            messageIsAMessage,
		"a sender is not part of an owed mail":         senderIsNotStored,
		"a foreign person still owes the mail":         personNeedsNoRow,
		"an owed mail is queued and unsent":            freshIntentState,
		"no state outside the six is answered":         stateIsKnown,
		"a message over the bound is refused":          oversizedMessage,
		"the recipient comes back as it was spelled":   recipientTrimmed,
		"a cause over the bound is refused":            causeTooLong,
		"the same cause under another purpose is owed": purposeDistinguishes,
	}
}

// outgoing is the mail every case starts from: a cause, a person, a notice, and a
// message that says something.
func outgoing() contracts.OutgoingMail {
	return contracts.OutgoingMail{
		Purpose: "billing.invoice_ready", Cause: uuid.NewString(), Person: Ada, Notice: uuid.New(),
		Message: contracts.Message{To: AdaEmail, Subject: "Your invoice", Body: "It is ready."},
	}
}

func queueOncePerCause(t *testing.T, f IntentFixture) {
	mail := outgoing()
	first, created, err := f.Intents.Queue(f.Ctx, f.Tx, mail)
	if err != nil || !created {
		t.Fatalf("queue the first mail: %v", err)
	}
	second, again, err := f.Intents.Queue(f.Ctx, f.Tx, mail)
	if err != nil {
		t.Fatalf("queue it again: %v", err)
	}
	if again {
		t.Error("the same (purpose, cause) was reported as a second owed mail")
	}
	if second.ID != first.ID {
		t.Errorf("the repeat returned intent %s, want the one already owed (%s)", second.ID, first.ID)
	}
}

func queueWithoutCause(t *testing.T, f IntentFixture) {
	mail := outgoing()
	mail.Cause = ""
	first, _, err := f.Intents.Queue(f.Ctx, f.Tx, mail)
	if err != nil {
		t.Fatalf("queue the first mail: %v", err)
	}
	second, created, err := f.Intents.Queue(f.Ctx, f.Tx, mail)
	if err != nil {
		t.Fatalf("queue the second mail: %v", err)
	}
	if !created || second.ID == first.ID {
		t.Errorf("a mail that names no cause was deduplicated to %s: an empty cause is no identity", second.ID)
	}
}

func purposeIsAName(t *testing.T, f IntentFixture) {
	for _, purpose := range []string{"Set Password", "", "a b", "Billing", "9lives"} {
		mail := outgoing()
		mail.Purpose = purpose
		if _, _, err := f.Intents.Queue(f.Ctx, f.Tx, mail); err != contracts.ErrMailPurpose {
			t.Errorf("purpose %q was refused with %v, want contracts.ErrMailPurpose", purpose, err)
		}
	}
}

func causeTooLong(t *testing.T, f IntentFixture) {
	mail := outgoing()
	mail.Cause = string(make([]byte, 201))
	if _, _, err := f.Intents.Queue(f.Ctx, f.Tx, mail); err != contracts.ErrMailCause {
		t.Errorf("a 201-byte cause was refused with %v, want contracts.ErrMailCause", err)
	}
}

func purposeDistinguishes(t *testing.T, f IntentFixture) {
	mail := outgoing()
	if _, _, err := f.Intents.Queue(f.Ctx, f.Tx, mail); err != nil {
		t.Fatalf("queue the first mail: %v", err)
	}
	mail.Purpose = "billing.payment_received"
	other, created, err := f.Intents.Queue(f.Ctx, f.Tx, mail)
	if err != nil || !created {
		t.Fatalf("the same cause under another purpose was refused with %v: a cause is shared across purposes", err)
	}
	_ = other
}

func recipientIsRequired(t *testing.T, f IntentFixture) {
	for _, to := range []string{"", "   "} {
		mail := outgoing()
		mail.Message.To = to
		if _, _, err := f.Intents.Queue(f.Ctx, f.Tx, mail); err != contracts.ErrMailRecipient {
			t.Errorf("recipient %q was refused with %v, want contracts.ErrMailRecipient", to, err)
		}
	}
	mail := outgoing()
	mail.Message.To = longAddress()
	if _, _, err := f.Intents.Queue(f.Ctx, f.Tx, mail); err != contracts.ErrMailRecipient {
		t.Errorf("a 321-byte address was refused with %v, want contracts.ErrMailRecipient", err)
	}
}

func recipientTrimmed(t *testing.T, f IntentFixture) {
	mail := outgoing()
	mail.Message.To = "  " + AdaEmail + "  "
	row, _, err := f.Intents.Queue(f.Ctx, f.Tx, mail)
	if err != nil {
		t.Fatalf("queue the mail: %v", err)
	}
	if row.Recipient != AdaEmail {
		t.Errorf("the recipient came back %q, want it trimmed to %q", row.Recipient, AdaEmail)
	}
}

func messageIsAMessage(t *testing.T, f IntentFixture) {
	mail := outgoing()
	mail.Message.Body = ""
	if _, _, err := f.Intents.Queue(f.Ctx, f.Tx, mail); err != contracts.ErrMailMessage {
		t.Errorf("a mail with no text body was refused with %v, want contracts.ErrMailMessage", err)
	}
}

func oversizedMessage(t *testing.T, f IntentFixture) {
	mail := outgoing()
	mail.Message.Body = string(make([]byte, contracts.MaxMailIntentBytes))
	if _, _, err := f.Intents.Queue(f.Ctx, f.Tx, mail); err != contracts.ErrMailSize {
		t.Errorf("a mail over the size bound was refused with %v, want contracts.ErrMailSize", err)
	}
}

func senderIsNotStored(t *testing.T, f IntentFixture) {
	mail := outgoing()
	mail.Message.Sender = &contracts.Sender{FromName: "Acme", Domain: "acme.example",
		Selector: "selector1", FromAddress: "notifications@acme.example", Status: contracts.SenderVerified}
	row, _, err := f.Intents.Queue(f.Ctx, f.Tx, mail)
	if err != nil {
		t.Fatalf("queue the mail: %v", err)
	}
	if row.Body == "" {
		t.Fatal("the message was not stored at all")
	}
}

func personNeedsNoRow(t *testing.T, f IntentFixture) {
	mail := outgoing()
	mail.Person = uuid.New()
	row, created, err := f.Intents.Queue(f.Ctx, f.Tx, mail)
	if err != nil || !created || row.Person != mail.Person {
		t.Errorf("a mail about a person this tenant has no row for: %+v, %v", err, created)
	}
}

func freshIntentState(t *testing.T, f IntentFixture) {
	row, _, err := f.Intents.Queue(f.Ctx, f.Tx, outgoing())
	if err != nil {
		t.Fatalf("queue the mail: %v", err)
	}
	if row.State != contracts.MailIntentQueued || row.Attempt != 0 || row.Uncertain != 0 {
		t.Errorf("a fresh intent is %s at attempt %d, uncertain %d, want queued/0/0", row.State, row.Attempt, row.Uncertain)
	}
	if row.Reason != "" || row.AttemptedAt != nil {
		t.Errorf("a fresh intent says %q and was attempted at %v: nothing has happened to it yet", row.Reason, row.AttemptedAt)
	}
}

func stateIsKnown(t *testing.T, f IntentFixture) {
	row, _, err := f.Intents.Queue(f.Ctx, f.Tx, outgoing())
	if err != nil {
		t.Fatalf("queue the mail: %v", err)
	}
	switch row.State {
	case contracts.MailIntentQueued, contracts.MailIntentInFlight, contracts.MailIntentSent,
		contracts.MailIntentFailed, contracts.MailIntentSuppressed, contracts.MailIntentUnknown:
	default:
		t.Errorf("the port wrote state %q, which is not one of the six the table allows", row.State)
	}
}

// longAddress is one byte past the address bound of 000076.
func longAddress() string {
	local := make([]byte, 321-len("acme.example.com")-1)
	for i := range local {
		local[i] = 'a'
	}
	return string(local) + "@acme.example.com"
}
