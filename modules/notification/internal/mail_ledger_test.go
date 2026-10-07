package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

// sent is a record of a mail the transport took: no reason to give.
func sent(kind, to, request string) contracts.MailRecord {
	return contracts.MailRecord{Kind: kind, Recipient: to, Outcome: contracts.MailSent, RequestID: request}
}

// TestTheMailLedgerAnswersTheRequestThatAsked is the read this table exists for:
// the newest outcome recorded for one request id, no row for a request that
// mailed nothing, and no answer at all for the empty id — which must never be
// read as "the newest untraced row in the tenant", because that row is somebody
// else's call.
func TestTheMailLedgerAnswersTheRequestThatAsked(t *testing.T) {
	admin, conn := dbtest.Schema(t, notification.Migrations)
	svc := internal.NewService(directory{})
	const (
		first  = "00000000-0000-4000-8000-0000000000a1"
		second = "00000000-0000-4000-8000-0000000000a2"
	)
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.RecordMail(ctx, tx, sent("auth.verification", "ada@example.com", first)); err != nil {
			return err
		}
		// Two attempts for one request id are two rows, and the answer is the last.
		if err := svc.RecordMail(ctx, tx, contracts.MailRecord{
			Kind: "auth.verification", Recipient: "ada@example.com", Outcome: contracts.MailSent,
			Traceparent: "00-000000000000000000000000000000b1-0000000000000001-01", RequestID: second,
		}); err != nil {
			return err
		}
		if err := svc.RecordMail(ctx, tx, contracts.MailRecord{
			Kind: "auth.verification", Recipient: "ada@example.com", Outcome: contracts.MailFailed,
			Reason: "the relay refused", RequestID: second,
		}); err != nil {
			return err
		}
		if got, known, err := svc.MailOutcome(ctx, tx, first); err != nil || !known || got != contracts.MailSent {
			t.Errorf("the first request's mail=%q known=%t err=%v, want sent true", got, known, err)
		}
		if got, known, err := svc.MailOutcome(ctx, tx, second); err != nil || !known || got != contracts.MailFailed {
			t.Errorf("newest of two=%q known=%t err=%v, want failed true", got, known, err)
		}
		if got, known, err := svc.MailOutcome(ctx, tx, "no-such-request"); err != nil || known || got != "" {
			t.Errorf("a request that mailed nothing=%q known=%t err=%v, want the empty answer", got, known, err)
		}
		if _, known, err := svc.MailOutcome(ctx, tx, ""); !errors.Is(err, contracts.ErrMailRequest) || known {
			t.Errorf("empty request id known=%t err=%v, want the caller's mistake refused", known, err)
		}
		// A mail with no call behind it — a job's, a replay's — is ordinary, and it
		// says so the way kit/events.Publish says the same thing: NULL, not the empty
		// string, so "which of these rows were never traced" is a query.
		return svc.RecordMail(ctx, tx, contracts.MailRecord{
			Kind: "auth.verification", Recipient: "ada@example.com", Outcome: contracts.MailSent,
		})
	})
	if err != nil {
		t.Fatalf("record and read: %v", err)
	}
	var n int
	if err := admin.QueryRow("SELECT count(*) FROM direct_mail_deliveries" +
		" WHERE traceparent IS NULL AND request_id IS NULL AND outcome = 'sent'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("untraced rows=%d, want the one send that had no call behind it", n)
	}
}

// TestTheMailLedgerRefusesWhatTheTableRefuses pins that the port runs the table's
// own rules: a caller cannot reach a CHECK by getting past the method, and the
// fake's caller and Postgres's caller are refused the same record.
func TestTheMailLedgerRefusesWhatTheTableRefuses(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	svc := internal.NewService(directory{})
	cases := []struct {
		name  string
		wrong contracts.MailRecord
		want  error
	}{
		{"a mail that did not go says why", contracts.MailRecord{
			Kind: "auth.set_password", Recipient: "ada@example.com", Outcome: contracts.MailFailed,
		}, contracts.ErrMailReason},
		{"a mail with no address", contracts.MailRecord{
			Kind: "auth.set_password", Outcome: contracts.MailFailed, Reason: "refused",
		}, contracts.ErrMailRecipient},
		{"a kind that is a sentence", contracts.MailRecord{
			Kind: "Set the password", Recipient: "ada@example.com", Outcome: contracts.MailSent,
		}, contracts.ErrMailKind},
		{"the ledger's vocabulary", contracts.MailRecord{
			Kind: "auth.set_password", Recipient: "ada@example.com", Outcome: "requested",
		}, contracts.ErrMailOutcome},
		{"an outcome nobody records", contracts.MailRecord{
			Kind: "auth.set_password", Recipient: "ada@example.com", Outcome: "",
		}, contracts.ErrMailOutcome},
	}
	for _, c := range cases {
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return svc.RecordMail(ctx, tx, c.wrong)
		})
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err=%v, want %v", c.name, err, c.want)
		}
	}
}

// TestAMailRecordCommitsWithItsSendOrNotAtAll is the half the fake cannot share:
// the row is written in the caller's transaction, so an attempt that did not
// commit leaves no record claiming it did.
func TestAMailRecordCommitsWithItsSendOrNotAtAll(t *testing.T) {
	admin, conn := dbtest.Schema(t, notification.Migrations)
	svc := internal.NewService(directory{})
	rollback := errors.New("the send is being rolled back")
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.RecordMail(ctx, tx, sent("auth.set_password", "ada@example.com", "req-rolled-back")); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("the transaction did not roll back: %v", err)
	}
	var n int
	if err := admin.QueryRow("SELECT count(*) FROM direct_mail_deliveries").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a rolled-back send left %d records", n)
	}
}

// TestAnotherTenantReadsNoMailRecord is the row-level security this table asks for:
// a request id from another tenant's call is a request that mailed nothing here,
// answered the same way and not refused differently.
func TestAnotherTenantReadsNoMailRecord(t *testing.T) {
	admin, conn := dbtest.Schema(t, notification.Migrations)
	svc := internal.NewService(directory{})
	const request = "00000000-0000-4000-8000-0000000000c3"
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.RecordMail(ctx, tx, sent("auth.verification", "ada@example.com", request))
	})
	if err != nil {
		t.Fatalf("record in acme: %v", err)
	}
	err = db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if got, known, err := svc.MailOutcome(ctx, tx, request); err != nil || known || got != "" {
			t.Errorf("globex reads acme's mail delivery: %q known=%t err=%v", got, known, err)
		}
		// And globex cannot write a row into acme: the tenant transaction can only
		// ever name its own tenant, which is what the WITH CHECK policy is for.
		return svc.RecordMail(ctx, tx, sent("auth.verification", "bob@example.com", "globex-owns-this"))
	})
	if err != nil {
		t.Fatalf("record in globex: %v", err)
	}
	var n int
	if err := admin.QueryRow("SELECT count(*) FROM direct_mail_deliveries WHERE request_id = $1", request).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("rows for acme's request=%d, want 1", n)
	}
}

// TestARedactedReasonCarriesNoSecret is the promise the record's reason column
// makes: the transport's own words, minus anything it quoted back.
func TestARedactedReasonCarriesNoSecret(t *testing.T) {
	const token = "Zm9vYmFyYmF6AQT3yZ0pQ0d0dGhlIHF1aWNrIGJyb3duIGZveA"
	said := "transport echoed its input: https://acme.example.com/auth/reset?token=" + token +
		strings.Repeat(" \n\t", 400)
	got := contracts.RedactMailReason(said, token)
	if strings.Contains(got, token) {
		t.Errorf("the redacted reason still carries the token: %q", got)
	}
	if len(got) > contracts.MaxMailReason {
		t.Errorf("the redacted reason is %d bytes, over the bound of %d", len(got), contracts.MaxMailReason)
	}
	if strings.ContainsAny(got, "\n\t") {
		t.Errorf("the redacted reason is still multi-line: %q", got)
	}
	if !strings.HasSuffix(got, "[redacted]") {
		t.Errorf("the redacted reason does not say what it removed: %q", got)
	}
	// A sentence that ends mid-rune is not a sentence: the clip is a rune
	// boundary, so the last byte sequence has to decode.
	long := strings.Repeat("é", contracts.MaxMailReason)
	if clipped := contracts.RedactMailReason(long); !strings.HasSuffix(clipped, "é") || len(clipped) > contracts.MaxMailReason {
		t.Errorf("clip ended off a rune boundary or over the bound: %d bytes", len(clipped))
	}
	// An empty secret is not a secret to remove, and must not delete the message.
	if got := contracts.RedactMailReason("the relay refused", ""); got != "the relay refused" {
		t.Errorf("an empty secret rewrote the sentence: %q", got)
	}
}
