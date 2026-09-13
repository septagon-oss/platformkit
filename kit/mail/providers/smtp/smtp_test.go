package smtp

import (
	"errors"
	"github.com/septagon-oss/platformkit/kit/mail"
	"net"
	"testing"
	"time"
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
	s := New(Mail{Host: host, Port: port, From: "noreply@acme.example.com"})

	start := time.Now()
	err := s.Send(t.Context(), mail.Message{
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
	timeout, ok := errors.AsType[net.Error](err)
	if !ok || !timeout.Timeout() {
		t.Errorf("the failure is %v, want a timeout the log can name", err)
	}
}
