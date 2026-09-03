package internal

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// Mail is one outgoing mail server, shaped like config.Mail so that main
// converts one to the other in a line and this module depends on a struct of
// its own — the arrangement modules/auth has with its OIDC provider.
type Mail struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// SMTP is the one production Mailer: stdlib net/smtp, STARTTLS when the server
// offers it, authentication only when a username is configured. There is no
// provider abstraction, because SMTP is what every service worth naming speaks.
type SMTP struct{ cfg Mail }

// mailStep bounds the dial, and then every command and every reply of the
// conversation after it.
//
// net/smtp has no context and no clock of its own: it is a synchronous
// conversation over whatever net.Conn it is handed, and it waits as long as
// that conn will. So a mail server that accepts the connection and then says
// nothing — a relay that has wedged, a firewall that black-holes the reply —
// used to cost the worker a goroutine and a socket per attempt, forever, with
// nothing logged: the send never returned, so the outbox never learned it had
// failed, so it never retried and never gave up. A deadline is what turns that
// silence into an error the retry ladder and the dead letter can see.
//
// It is per step and refreshed on each read and write rather than per message,
// so a large body over a slow link is not cut off half way through while a
// server that has stopped answering still is. Thirty seconds is generous for
// one SMTP command and short enough that a wedged relay is a dead letter within
// the hour rather than a leak.
//
// A variable rather than a constant only so that smtp_test.go can hold a server
// hung for milliseconds; nothing outside this package can see it.
var mailStep = 30 * time.Second

// paced is a net.Conn that pushes its deadline out on every read and write,
// which is how mailStep reaches a library that takes no context. STARTTLS wraps
// this conn rather than replacing it, so the deadline survives the upgrade.
type paced struct {
	net.Conn
}

func (p paced) Read(b []byte) (int, error) {
	if err := p.Conn.SetDeadline(time.Now().Add(mailStep)); err != nil {
		return 0, err
	}
	return p.Conn.Read(b)
}

func (p paced) Write(b []byte) (int, error) {
	if err := p.Conn.SetDeadline(time.Now().Add(mailStep)); err != nil {
		return 0, err
	}
	return p.Conn.Write(b)
}

// NewSMTP returns the sender for cfg. main wires notification.Mailbox
// instead when no host is configured, and says so at boot.
func NewSMTP(cfg Mail) *SMTP { return &SMTP{cfg: cfg} }

var _ contracts.Mailer = (*SMTP)(nil)

// Send delivers one message. The connection is opened, used and closed per
// message: this runs in the worker, one event at a time, so a pool would buy
// nothing. A send that fails is retried by the outbox rather than here, because
// the kernel owns the retry ladder and two of them would be two policies — and
// mailStep is what guarantees a send fails at all rather than waiting forever.
func (s *SMTP) Send(ctx context.Context, m contracts.Message) error {
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	d := net.Dialer{Timeout: mailStep}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("notification: dial %s: %w", addr, err)
	}
	// The greeting is a read, so it is already on the clock: a server that
	// accepts and never speaks fails here rather than never.
	c, err := smtp.NewClient(paced{conn}, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("notification: greet %s: %w", addr, err)
	}
	defer func() { _ = c.Close() }()

	// Not demanded, because the relay a small deployment runs is often on a
	// private network; when it is offered, failing to negotiate is an error and
	// never a silent fall back to plaintext.
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host}); err != nil {
			return fmt.Errorf("notification: start TLS with %s: %w", s.cfg.Host, err)
		}
	}
	if s.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("notification: authenticate to %s: %w", s.cfg.Host, err)
		}
	}
	if err := c.Mail(s.cfg.From); err != nil {
		return fmt.Errorf("notification: sender %s refused: %w", s.cfg.From, err)
	}
	if err := c.Rcpt(m.To); err != nil {
		return fmt.Errorf("notification: recipient refused: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("notification: send the body: %w", err)
	}
	if _, err := w.Write([]byte(wire(s.cfg.From, m))); err != nil {
		return fmt.Errorf("notification: write the body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("notification: finish the body: %w", err)
	}
	return c.Quit()
}

// wire is the message as RFC 5322 wants it: headers, a blank line, the body,
// CRLF throughout. The subject is folded into one line because a newline in it
// would be a header somebody else wrote.
func wire(from string, m contracts.Message) string {
	subject := strings.Join(strings.Fields(m.Subject), " ")
	head := "From: " + from + "\r\nTo: " + m.To + "\r\nSubject: " + subject +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n"
	return head + strings.ReplaceAll(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n", "\r\n")
}
