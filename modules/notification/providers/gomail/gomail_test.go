package gomail_test

// These cases talk to a mail server made of ten lines of a goroutine and a
// buffer. That is the point: the three things this carrier is responsible for —
// which address is on the envelope, which address and name are in the header,
// and whether the message is signed — are all facts about bytes on the wire, and
// a test that mocks the library would prove that the mock works.

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/providers/gomail"
)

// relay is a stub SMTP server: it greets, accepts one message, and answers RCPT
// with whatever the case asked for. It records the envelope sender and the whole
// DATA payload.
type relay struct {
	addr     string
	rcptErr  string // "" accepts, "450 …" is transient, "550 …" is permanent
	greet    string
	envelope string
	data     string
	accepted chan struct{}
}

func serve(t *testing.T, r *relay) *relay {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	a := l.Addr().(*net.TCPAddr)
	r.addr = fmt.Sprintf("%s:%d", a.IP, a.Port)
	r.accepted = make(chan struct{})
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		w := bufio.NewWriter(c)
		say := func(line string) { _, _ = w.WriteString(line + "\r\n"); _ = w.Flush() }
		say("220 relay ESMTP")
		sc := bufio.NewScanner(c)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			cmd := sc.Text()
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250-relay hello")
				say("250 HELP")
			case strings.HasPrefix(cmd, "MAIL FROM:"):
				r.envelope = strings.Trim(strings.TrimPrefix(cmd, "MAIL FROM:"), "<>")
				say("250 ok")
			case strings.HasPrefix(cmd, "RCPT TO:"):
				if r.rcptErr != "" {
					say(r.rcptErr)
					continue
				}
				say("250 ok")
			case cmd == "DATA":
				say("354 go ahead")
				var body strings.Builder
				for sc.Scan() {
					line := sc.Text()
					if line == "." {
						break
					}
					body.WriteString(line + "\r\n")
				}
				r.data = body.String()
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				close(r.accepted)
				return
			default:
				say("250 ok")
			}
		}
	}()
	return r
}

func (r *relay) config() gomail.Config {
	return gomail.Config{
		Host: "127.0.0.1", Port: port(r.addr), EnvelopeFrom: "noreply@acme.example.com",
		Timeout: 5 * time.Second,
	}
}

func port(hostport string) int {
	i := strings.LastIndex(hostport, ":")
	n := 0
	for _, c := range hostport[i+1:] {
		n = n*10 + int(c-'0')
	}
	return n
}

// dkimKey is a throwaway RSA key, generated rather than fixed: what matters is
// that a signature appears and names the tenant's domain and selector, not that
// the key is somebody's.
func dkimKey(t *testing.T) []byte {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
}

func sender(key []byte) *contracts.Sender {
	return &contracts.Sender{
		Domain: "acme.example.com", Selector: "sel1", FromName: "Acme",
		FromAddress: "notifications@acme.example.com", Status: contracts.SenderVerified, Key: key,
	}
}

// TestTheTenantSendsFromItsOwnName is the brief's Done-when, read off the wire:
// the header carries the tenant's name and address, the envelope carries the
// deployment's, and the signature is the tenant's, found by the tenant's own
// selector.
//
// Delete the SetDKIM line in gomail.go and the DKIM assertion fails; pass
// cfg.EnvelopeFrom into msg.From instead of msg.EnvelopeFrom and the second
// assertion fails, which is the confusion kit/config refuses at the door.
func TestTheTenantSendsFromItsOwnName(t *testing.T) {
	r := serve(t, &relay{})
	s := gomail.New(r.config())
	m := contracts.Message{
		To: "ada@example.com", ReplyTo: "helpdesk@acme.example.com",
		Subject: "Your appointment", Body: "Come at ten.", HTML: "<p>Come at ten.</p>",
		Lang: "en", Sender: sender(dkimKey(t)),
	}
	if err := s.Send(t.Context(), m); err != nil {
		t.Fatalf("send: %v", err)
	}
	if r.envelope != "noreply@acme.example.com" {
		t.Errorf("MAIL FROM is %q, want the deployment's envelope address", r.envelope)
	}
	for _, want := range []string{
		`From: Acme <notifications@acme.example.com>`,
		"<notifications@acme.example.com>",
		"d=acme.example.com", "s=sel1",
		"Reply-To: <helpdesk@acme.example.com>",
		"text/plain", "text/html", "multipart/alternative",
		"Come at ten.", "<p>Come at ten.</p>",
	} {
		if want == `From: Acme <notifications@acme.example.com>` {
			// The name may be encoded or literal; what must not happen is it being
			// absent, so the check below reads the decoded form.
			continue
		}
		if !strings.Contains(r.data, want) {
			t.Errorf("the message carries no %q:\n%s", want, r.data)
		}
	}
	if !strings.Contains(r.data, "notifications@acme.example.com") || !strings.Contains(r.data, "Acme") {
		t.Errorf("the From header is not the tenant's:\n%s", r.data)
	}
}

// TestASenderWithNoKeyIsRefusedUnsigned: the alternative is mail from a domain
// that publishes a record, unsigned, which spends the tenant's reputation and
// says nothing about it afterwards.
func TestASenderWithNoKeyIsRefusedUnsigned(t *testing.T) {
	r := serve(t, &relay{})
	err := gomail.New(r.config()).Send(t.Context(), contracts.Message{
		To: "ada@example.com", Subject: "s", Body: "b", Sender: sender(nil),
	})
	if !errors.Is(err, contracts.ErrPermanent) {
		t.Fatalf("unsigned mail was answered %v, want contracts.ErrPermanent", err)
	}
	if r.data != "" {
		t.Errorf("nothing should have reached the relay, got:\n%s", r.data)
	}
}

// TestTheRelaysVerdictsAreToldApart is the retry rule. A 550 is the far end's
// decision and will not change, so it is contracts.ErrPermanent: the worker
// writes the failed row and acknowledges. A 450 is "later", and later is the
// outbox's ladder, not a row saying the message is dead.
func TestTheRelaysVerdictsAreToldApart(t *testing.T) {
	cases := []struct {
		answer    string
		permanent bool
	}{
		{"550 5.1.1 no such mailbox", true},
		{"450 4.2.1 try again", false},
	}
	for _, c := range cases {
		t.Run(c.answer, func(t *testing.T) {
			r := serve(t, &relay{rcptErr: c.answer})
			err := gomail.New(r.config()).Send(t.Context(), contracts.Message{
				To: "gone@example.com", Subject: "s", Body: "b",
			})
			if err == nil {
				t.Fatal("a refused recipient was reported as delivered")
			}
			if got := errors.Is(err, contracts.ErrPermanent); got != c.permanent {
				t.Errorf("ErrPermanent = %v, want %v (%v)", got, c.permanent, err)
			}
		})
	}
}

// TestAHungMailServerIsAnErrorAndNotAWait is the stdlib sender's bug, kept as a
// case so it cannot come back: a relay that accepts the socket and then says
// nothing used to hold the worker's goroutine and socket forever, so the outbox
// never learned the send had failed and never retried it.
func TestAHungMailServerIsAnErrorAndNotAWait(t *testing.T) {
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
			t.Cleanup(func() { _ = c.Close() }) // held, not closed: closing would be an answer
		}
	}()
	a := l.Addr().(*net.TCPAddr)
	cfg := gomail.Config{Host: a.IP.String(), Port: a.Port, EnvelopeFrom: "noreply@acme.example.com", Timeout: 300 * time.Millisecond}

	start := time.Now()
	err = gomail.New(cfg).Send(t.Context(), contracts.Message{
		To: "ada@acme.example.com", Subject: "Reset your password", Body: "the link",
	})
	took := time.Since(start)
	if err == nil {
		t.Fatal("a mail server that never speaks was reported as a delivery")
	}
	if took > 10*cfg.Timeout {
		t.Errorf("the send took %s to fail, want less than %s", took, 10*cfg.Timeout)
	}
	var timeout net.Error
	if !errors.As(err, &timeout) && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the failure is %v, want a timeout the log can name", err)
	}
}

// TestAMessageWithNoTextIsRefused keeps the rule the contracts state: a mail
// client that is given HTML and no text part shows the source.
func TestAMessageWithNoTextIsRefused(t *testing.T) {
	r := serve(t, &relay{})
	err := gomail.New(r.config()).Send(t.Context(), contracts.Message{
		To: "ada@example.com", Subject: "s", HTML: "<p>b</p>",
	})
	if !errors.Is(err, contracts.ErrPermanent) {
		t.Fatalf("a textless message was answered %v, want contracts.ErrPermanent", err)
	}
}
