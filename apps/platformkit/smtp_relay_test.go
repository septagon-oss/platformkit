package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"

	"github.com/septagon-oss/platformkit/kit/config"
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
// Reading a link out of this relay instead of out of the recording mailbox changes
// nothing about what a case asserts: the property is the address in the link and
// the acknowledgment around it, not which object kept the message.
type smtpRelay struct {
	ln net.Listener

	mu    sync.Mutex
	kept  []RelayMessage
	alive bool
}

// RelayMessage is one message the relay accepted: the address it was addressed to
// and the body it was given, as it arrived on the wire.
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
	r := &smtpRelay{ln: ln, alive: true}
	go r.serve()
	t.Cleanup(func() {
		r.mu.Lock()
		r.alive = false
		r.mu.Unlock()
		_ = ln.Close()
	})
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
			r.kept = append(r.kept, RelayMessage{To: to, Body: body})
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
