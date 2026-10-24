package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"

	"github.com/septagon-oss/platformkit/kit/config"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/providers/gomail"
)

// smtpRelay is an SMTP server on loopback that accepts everything and keeps what it
// was handed, for the cases that stand in the installation which named a mail host.
//
// It is here rather than in notificationtest because it is a test fixture of this
// one package: the only other relay in the tree is a _test.go local of
// providers/gomail, which nothing else can compose. It is stdlib and plaintext on
// purpose — the carrier runs STARTTLS when the server offers it and sends plainly
// when it does not, and it authenticates only when the deployment names a username,
// so a conversation with no TLS and no AUTH is the conversation a private relay has.
//
// What it hands back is the message the sender wrote, not the bytes the transport
// carried: reading a link out of this relay instead of out of the recording mailbox
// then changes nothing about what a case asserts, because both answer the same
// question — the address in the link and the acknowledgment around it — and neither
// one is the object that kept the message. messageText is the work that makes that
// true, and TestTheRelayPresentsTheMessageTheSenderWrote is what checks it.
type smtpRelay struct {
	ln net.Listener

	mu   sync.Mutex
	kept []RelayMessage
}

// RelayMessage is one message the relay accepted: the address it was addressed to
// and the message as the person's mail client reads it, which is what a case
// reading a link out of it is asking about. See messageText for why that is not
// the bytes the relay was handed.
type RelayMessage struct {
	To   string
	Body string
}

// startSMTPRelay listens on loopback until the case ends and returns what it kept.
func startSMTPRelay(t *testing.T) *smtpRelay {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for mail on loopback: %v", err)
	}
	r := &smtpRelay{ln: ln}
	go r.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return r
}

// host and port are the address the deployment's mail.host and mail.port name.
func (r *smtpRelay) host() string { return r.ln.Addr().(*net.TCPAddr).IP.String() }
func (r *smtpRelay) port() int    { return r.ln.Addr().(*net.TCPAddr).Port }

// Sent is every message accepted so far, in the order it arrived.
func (r *smtpRelay) Sent() []RelayMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RelayMessage(nil), r.kept...)
}

func (r *smtpRelay) serve() {
	for {
		conn, err := r.ln.Accept()
		if err != nil {
			return // the listener closed with the case
		}
		go r.talk(conn)
	}
}

// talk answers one SMTP conversation: enough of RFC 5321 for a sender that dials,
// starts no TLS it was not offered, authenticates nothing and quits. Anything it
// does not understand is refused rather than ignored, so a case that asked for
// something this relay cannot answer learns so at once.
func (r *smtpRelay) talk(conn net.Conn) {
	defer conn.Close()
	w := bufio.NewWriter(conn)
	line := bufio.NewReader(conn)
	to := ""
	say := func(format string, args ...any) bool {
		if _, err := fmt.Fprintf(w, format+"\r\n", args...); err != nil {
			return false
		}
		return w.Flush() == nil
	}
	if !say("220 pkit-relay ready") {
		return
	}
	for {
		text, err := line.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(text))
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			// One greeting, in the multiline form the protocol requires: every line
			// but the last carries a hyphen where the code's space would be. Two
			// final lines would be two responses to one command, and the sender would
			// read its next answer out of the greeting. The list says nothing this
			// relay has: no STARTTLS it would not honour, no AUTH it was not given.
			if !say("250-pkit-relay") || !say("250 HELP") {
				return
			}
		case strings.HasPrefix(cmd, "HELO"):
			if !say("250 pkit-relay") {
				return
			}
		case strings.HasPrefix(cmd, "MAIL FROM"):
			if !say("250 ok") {
				return
			}
		case strings.HasPrefix(cmd, "RCPT TO"):
			to = addressIn(text)
			if !say("250 ok") {
				return
			}
		case cmd == "DATA":
			if !say("354 go ahead") {
				return
			}
			body, err := readData(line)
			if err != nil {
				return
			}
			r.mu.Lock()
			r.kept = append(r.kept, RelayMessage{To: to, Body: messageText(body)})
			r.mu.Unlock()
			if !say("250 queued") {
				return
			}
		case cmd == "RSET", cmd == "NOOP":
			if !say("250 ok") {
				return
			}
		case strings.HasPrefix(cmd, "QUIT"):
			say("221 bye")
			return
		default:
			if !say("500 unrecognized command") {
				return
			}
		}
	}
}

// readData reads one message until the dot on its own line, which is where the
// transport says the message ends.
func readData(r *bufio.Reader) (string, error) {
	var out strings.Builder
	for {
		text, err := r.ReadString('\n')
		if err != nil {
			return "", err
		}
		if strings.TrimRight(text, "\r\n") == "." {
			return out.String(), nil
		}
		out.WriteString(text)
	}
}

// messageText is the text of one DATA payload as the recipient's mail client shows it:
// the headers read, the text part chosen and its transfer encoding undone. A case that
// takes a link out of the wire bytes instead takes the sender's message with
// quoted-printable's marks inside it — a literal "=" arrives as "=3D" and a line over
// 75 characters is broken with "=\r\n" — so a token copied out of a raw body has two
// characters added in front of it, and sometimes one of its own broken in half. A
// payload this cannot read is kept as it arrived, so the case that finds no link in it
// says so in its own words rather than reading a body this fixture invented.
func messageText(data string) string {
	msg, err := mail.ReadMessage(strings.NewReader(data))
	if err != nil {
		return data
	}
	raw, err := io.ReadAll(msg.Body)
	if err != nil {
		return data
	}
	if media, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type")); err == nil &&
		strings.HasPrefix(media, "multipart/") {
		if text, ok := textPart(raw, params["boundary"]); ok {
			return text
		}
		return data
	}
	return transferDecoded(msg.Header.Get("Content-Transfer-Encoding"), raw)
}

// textPart is the decoded text/plain part of a multipart message, and whether it had
// one. The HTML alternative is not it: the carrier writes the text part first and
// refuses a message with no text body at all, and a person pastes a text link.
func textPart(raw []byte, boundary string) (string, bool) {
	reader := multipart.NewReader(bytes.NewReader(raw), boundary)
	for {
		part, err := reader.NextPart()
		if err != nil {
			return "", false
		}
		if media, _, err := mime.ParseMediaType(part.Header.Get("Content-Type")); err != nil || media != "text/plain" {
			continue
		}
		body, err := io.ReadAll(part)
		if err != nil {
			return "", false
		}
		return transferDecoded(part.Header.Get("Content-Transfer-Encoding"), body), true
	}
}

// transferDecoded undoes the encoding one part names. 7bit, 8bit and binary are the
// bytes as they arrived, which is what RFC 2045 says they are; anything unrecognised
// comes back as it came.
func transferDecoded(encoding string, raw []byte) string {
	var reader io.Reader
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		reader = quotedprintable.NewReader(bytes.NewReader(raw))
	case "base64":
		reader = base64.NewDecoder(base64.StdEncoding, bytes.NewReader(raw))
	default:
		return string(raw)
	}
	text, err := io.ReadAll(reader)
	if err != nil {
		return string(raw)
	}
	return string(text)
}

// addressIn is the address of an RCPT TO line, without its angle brackets.
func addressIn(line string) string {
	_, rest, found := strings.Cut(strings.ToLower(line), "to:")
	if !found {
		return ""
	}
	address := strings.TrimSpace(rest)
	address = strings.TrimPrefix(address, "<")
	address, _, _ = strings.Cut(address, ">")
	return address
}

// mailing remembers which installation each case asked for, the same way `copied`
// does: writeConfig reads it, so a case that names a mail host does not move the
// 122 cases that need the recording carrier exactly as it is.
var mailing sync.Map // *testing.T → *smtpRelay

// mailSection is the config this installation reads as "a mail host is configured",
// which is the same three keys the composition's own pick needs non-empty and the
// relay answers without a credential.
func mailSection(r *smtpRelay) string {
	return "mail:\n  host: \"" + r.host() + "\"\n  port: " + strconv.Itoa(r.port()) +
		"\n  from: \"platformkit@acme.localhost\"\n"
}

// configureMailing is configure in the installation that named a mail host: the
// relay on loopback is this deployment's SMTP server, and what it accepted is where
// a case reads the link the composition mailed.
func configureMailing(t *testing.T) (string, config.Config, *smtpRelay) {
	t.Helper()
	relay := startSMTPRelay(t)
	mailing.Store(t, relay)
	t.Cleanup(func() { mailing.Delete(t) })
	path, cfg := configure(t)
	return path, cfg, relay
}

// startedMailing is the same installation with its mail host named: booted, with
// the installation's own administrator signed in, mailing to the relay.
func startedMailing(t *testing.T) (config.Config, *http.Client, *smtpRelay) {
	t.Helper()
	path, cfg, relay := configureMailing(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))
	return cfg, signIn(t, cfg, acmeHost, adminEmail, adminPass), relay
}

// TestTheRelayPresentsTheMessageTheSenderWrote is this fixture's own guarantee: what a
// case reads out of the relay is the text the carrier put on the wire, to the
// character, for a message long enough that the transport encodes it and folds its
// lines. The carrier writes quoted-printable, which escapes the "=" of a query string
// as "=3D" and breaks a long line with "=\r\n", so a link read off the wire is a link
// with the encoding inside it and the credential no door will spend. Every case that
// redeems a mailed link reads it through here, so the guarantee is checked once, at
// the relay, with the real carrier rather than nine times at the doors.
func TestTheRelayPresentsTheMessageTheSenderWrote(t *testing.T) {
	relay := startSMTPRelay(t)
	const token = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"
	body := "Open http://acme.localhost:8080/app/auth/verify-email?token=" + token +
		" to confirm the account this installation just accepted. That link is the only"
	if err := gomail.New(gomail.Config{
		Host: relay.host(), Port: relay.port(), EnvelopeFrom: "platformkit@acme.localhost",
		Timeout: 5 * time.Second,
	}).Send(t.Context(), notificationcontracts.Message{
		To: "reader@acme.localhost", Subject: "Confirm your account", Body: body,
		HTML: "<p><a href=\"http://acme.localhost:8080/app/auth/verify-email?token=" + token + "\">confirm</a></p>",
	}); err != nil {
		t.Fatalf("send through the relay: %v", err)
	}
	sent := relay.Sent()
	if len(sent) != 1 {
		t.Fatalf("the relay kept %d messages, want the one it was sent", len(sent))
	}
	if sent[0].To != "reader@acme.localhost" {
		t.Errorf("the envelope it accepted names %q, want reader@acme.localhost", sent[0].To)
	}
	if sent[0].Body != body {
		t.Errorf("the relay presents %q for the text the sender wrote %q", sent[0].Body, body)
	}
	if at := regexp.MustCompile(`token=([A-Za-z0-9_-]+)`).FindStringSubmatch(sent[0].Body); len(at) != 2 ||
		at[1] != token {
		t.Errorf("the credential a case would copy out of it is %v, want %s", at, token)
	}
}
