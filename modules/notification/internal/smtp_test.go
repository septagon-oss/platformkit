package internal

// The one test that has to reach inside this package: the SMTP deadline, which
// is a package variable so that a hung server can be hung for milliseconds
// rather than for the thirty seconds the shipped one waits.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// hung is a listener that accepts a connection and never says anything, which
// is what a wedged relay looks like from here: the TCP handshake succeeds, so
// the dial timeout never fires, and then nothing arrives.
func hung(t *testing.T) (host string, port int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			// Held, not closed: closing would be an answer.
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	addr := l.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

// TestAHungMailServerIsAnErrorAndNotAWait. Before the deadline this call never
// returned: net/smtp waits as long as the conn will, so the worker leaked a
// goroutine and a socket per attempt, the outbox never learned the send had
// failed, and there was neither a retry nor a dead letter — the message simply
// never arrived and nothing said so.
//
// Deleting the Dialer timeout and the paced conn in smtp.go hangs this test
// until the package deadline kills it.
func TestAHungMailServerIsAnErrorAndNotAWait(t *testing.T) {
	was := mailStep
	mailStep = 150 * time.Millisecond
	t.Cleanup(func() { mailStep = was })

	host, port := hung(t)
	s := NewSMTP(Mail{Host: host, Port: port, From: "noreply@acme.example.com"})

	start := time.Now()
	err := s.Send(t.Context(), contracts.Message{
		To: "ada@acme.example.com", Subject: "Reset your password", Body: "the link",
	})
	took := time.Since(start)
	if err == nil {
		t.Fatal("a mail server that never speaks was reported as a delivery")
	}
	// The bound is what the retry ladder depends on: an attempt that does not
	// end is an attempt the outbox cannot count.
	if limit := 10 * mailStep; took > limit {
		t.Errorf("the send took %s to fail, want less than %s", took, limit)
	}
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Errorf("the failure is %v, want a timeout the log can name", err)
	}
}

// TestAMailerThatFailsFailsTheSubscription is the other half, and it is the
// half the dead letter needs: SendMail must hand the mailer's error back rather
// than swallow it, because an error is the only thing the outbox's ladder can
// count. What the ladder then does with it — four redeliveries and a row in
// platformkit_dead_letters — is kit/events' promise, and kit/events'
// TestAPoisonEventIsDeadLetteredAndStopsComingBack proves it in milliseconds,
// where the ladder's constants live.
func TestAMailerThatFailsFailsTheSubscription(t *testing.T) {
	// The module's own SQL, read from disk: this white-box test cannot import
	// the package that embeds it (modules/notification imports this one).
	_, conn := dbtest.Schema(t, db.MigrationSource{Owner: "notification", Files: os.DirFS("../migrations")})
	broken := errors.New("notification: dial mail.acme.example.com:587: i/o timeout")
	sub := SendMail(refusing{broken}, everybody{}, somewhere{}, true)
	svc := NewService(everybody{})
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: uuid.New(), Title: "Reset your password", Link: "/auth/reset", Email: true,
		})
		if err != nil {
			return err
		}
		payload, err := json.Marshal(contracts.EmailRequested{NotificationID: row.ID, Recipient: row.RecipientID})
		if err != nil {
			return err
		}
		handled := sub.Handler(ctx, tx, events.Event{
			ID: uuid.New(), Name: contracts.EventEmailRequested, Payload: payload,
		})
		if handled == nil {
			t.Error("a send that failed was reported as handled, so the outbox would never retry it")
		} else if !errors.Is(handled, broken) {
			t.Errorf("the subscription reported %v, want the mailer's own failure", handled)
		}
		return errRolledBack
	})
	if !errors.Is(err, errRolledBack) {
		t.Fatalf("the case's transaction: %v", err)
	}
}

var errRolledBack = errors.New("rolled back on purpose")

// The three collaborators the second case needs: a mailer that always refuses,
// a directory in which everybody has an address, and a host to build a link on.
type refusing struct{ err error }

func (r refusing) Send(context.Context, contracts.Message) error { return r.err }

type everybody struct{}

func (everybody) Email(context.Context, db.Tx[db.Tenant], uuid.UUID) (string, error) {
	return "ada@acme.example.com", nil
}

type somewhere struct{}

func (somewhere) PublicHost(context.Context, db.Tx[db.Tenant]) (string, error) {
	return "acme.example.com", nil
}

// recording is a mail server that accepts every message and hands back what the
// client said: each command line, and the message after DATA.
func recording(t *testing.T) (host string, port int, said <-chan []string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	out := make(chan []string, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		r, lines, data := bufio.NewReader(c), []string{}, false
		_, _ = c.Write([]byte("220 recording ESMTP\r\n"))
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				out <- lines
				return
			}
			line = strings.TrimRight(line, "\r\n")
			lines = append(lines, line)
			switch {
			case data && line == ".":
				data = false
				_, _ = c.Write([]byte("250 queued\r\n"))
			case data:
			case strings.HasPrefix(line, "EHLO"):
				_, _ = c.Write([]byte("250-recording\r\n250 8BITMIME\r\n"))
			case line == "DATA":
				data = true
				_, _ = c.Write([]byte("354 go ahead\r\n"))
			case line == "QUIT":
				_, _ = c.Write([]byte("221 bye\r\n"))
				out <- lines
				return
			default:
				_, _ = c.Write([]byte("250 ok\r\n"))
			}
		}
	}()
	addr := l.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, out
}

// TestTheEnvelopeIsTheBareAddressAndTheHeaderKeepsTheName. A sender written as
// "Acme <noreply@acme.example.com>" used to go into MAIL FROM whole, where every
// relay refuses it (Mailpit: "501 invalid FROM parameter"), so no message with a
// named sender was ever delivered. The envelope takes the address; the person
// sees the name.
func TestTheEnvelopeIsTheBareAddressAndTheHeaderKeepsTheName(t *testing.T) {
	host, port, said := recording(t)
	s := NewSMTP(Mail{Host: host, Port: port, From: "Acme <noreply@acme.example.com>"})
	if err := s.Send(t.Context(), contracts.Message{
		To: "ada@acme.example.com", Subject: "Verify your email address", Body: "the link",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	lines := <-said
	var envelope, header string
	for _, l := range lines {
		if strings.HasPrefix(l, "MAIL FROM:") {
			envelope = l
		}
		if strings.HasPrefix(l, "From: ") {
			header = l
		}
	}
	if !strings.HasPrefix(envelope, "MAIL FROM:<noreply@acme.example.com>") {
		t.Errorf("envelope = %q, want the bare address", envelope)
	}
	if header != `From: "Acme" <noreply@acme.example.com>` {
		t.Errorf("header = %q, want the display name and the address", header)
	}
}
