// Package gomailtest is the mail server an SMTP carrier can be pointed at in a
// test: one that listens on loopback, accepts everything and keeps what it was
// handed.
//
// It exists because a case that stands in the installation which named a mail
// host needs a server, not a recorder: the question "did this deployment dial
// anybody?" is answered by the process on the other end of the socket. It is
// stdlib and plaintext on purpose — the carrier runs STARTTLS when the server
// offers it and sends plainly when it does not, and it authenticates only when
// the deployment names a username, so a conversation with no TLS and no AUTH is
// the conversation a private relay has.
package gomailtest

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
	"net/mail"
	"strings"
	"sync"
	"testing"
)

// Relay is an SMTP server on loopback that accepts everything and keeps what it
// was handed. Start gives one to a case and closes it when the case ends.
type Relay struct {
	ln net.Listener

	mu   sync.Mutex
	kept []Message
}

// Message is one message the relay accepted: the address it was addressed to and
// the message as the person's mail client reads it, which is what a case reading
// a link out of it is asking about. See messageText for why that is not the bytes
// the relay was handed.
type Message struct {
	To   string
	Body string
}

// Start listens on a free loopback port until the case ends and returns the relay
// that is listening. It fails the case rather than returning a half-started
// server, because a case that cannot dial is not testing a mail host.
func Start(t testing.TB) *Relay {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for mail on loopback: %v", err)
	}
	r := &Relay{ln: ln}
	go r.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return r
}

// Host and Port are the address a deployment's mail.host and mail.port name.
func (r *Relay) Host() string { return r.ln.Addr().(*net.TCPAddr).IP.String() }
func (r *Relay) Port() int    { return r.ln.Addr().(*net.TCPAddr).Port }

// Sent is every message accepted so far, in the order it arrived.
func (r *Relay) Sent() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Message(nil), r.kept...)
}

func (r *Relay) serve() {
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
func (r *Relay) talk(conn net.Conn) {
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
			// read its next answer out of the greeting.
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
			r.kept = append(r.kept, Message{To: to, Body: messageText(body)})
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

// messageText is the text of one DATA payload as the recipient's mail client shows
// it: the headers read, the text part chosen and its transfer encoding undone.
// A case that takes a link out of the wire bytes instead takes the sender's
// message with quoted-printable's marks inside it — a literal "=" arrives as
// "=3D" and a long line is broken with "=\r\n" — so a token copied out of a raw
// body has two characters added in front of it.
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
