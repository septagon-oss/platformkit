package gomail_test

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/secrets"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/providers/gomail"
)

// mailPassword is the store this relay's credential comes from: the in-process
// store of the port, so the case still asserts what the relay sees and says nothing
// about which store a deployment runs. The trailing newline is on purpose: a mounted
// secret carries the one its producer wrote, and the relay must still be given the
// password.
func mailPassword(t *testing.T, password string) secrets.Secrets {
	t.Helper()
	return secrets.Map(map[secrets.Ref]secrets.Secret{
		secrets.Shared("mail.password"): {Bytes: []byte(password + "\n")},
	}, nil)
}

func TestConfiguredCredentialsAuthenticateBeforeMail(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	finished := make(chan bool, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			finished <- false
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		authenticated := false
		defer func() { finished <- authenticated }()
		say := func(line string) { _, _ = fmt.Fprintf(conn, "%s\r\n", line) }
		say("220 localhost ESMTP")
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			command := scanner.Text()
			switch {
			case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"):
				say("250-localhost")
				say("250 AUTH PLAIN LOGIN CRAM-MD5")
			case strings.HasPrefix(command, "AUTH "):
				authenticated = true
				say("235 Authentication successful")
			case strings.HasPrefix(command, "MAIL FROM:"):
				if !authenticated {
					say("530 Authentication required")
					continue
				}
				say("250 Sender accepted")
			case strings.HasPrefix(command, "RCPT TO:"):
				say("250 Recipient accepted")
			case command == "DATA":
				say("354 Send message")
				for scanner.Scan() {
					if scanner.Text() == "." {
						break
					}
				}
				say("250 Message accepted")
			case command == "QUIT":
				say("221 Bye")
				return
			default:
				say("250 OK")
			}
		}
	}()
	cfg := gomail.Config{Host: "localhost", Port: port(listener.Addr().String()), Username: "test-user",
		Secrets: mailPassword(t, "test-password"), SecretName: "mail.password",
		EnvelopeFrom: "sender@example.com", Timeout: 5 * time.Second}
	err = gomail.New(cfg).Send(t.Context(), contracts.Message{To: "recipient@example.com", Subject: "Authenticated mail", Body: "Message"})
	if err != nil {
		t.Errorf("configured SMTP credentials did not deliver through an AUTH-required relay: %v", err)
	}
	select {
	case authenticated := <-finished:
		if !authenticated {
			t.Error("relay received no AUTH command despite configured username and password")
		}
	case <-time.After(6 * time.Second):
		t.Fatal("SMTP session did not finish")
	}
}
