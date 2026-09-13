package memory_test

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/mail"
	"github.com/septagon-oss/platformkit/kit/mail/providers/memory"
)

func TestMailboxKeepsBodiesPrivateAndReturnsDetachedSnapshots(t *testing.T) {
	var output bytes.Buffer
	before := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(before) })
	box := memory.New()
	message := mail.Message{To: "recipient@example.test", Subject: "Account setup", Body: "secret-fixture-setup-link"}
	if err := box.Send(t.Context(), message); err != nil {
		t.Fatal(err)
	}
	snapshot := box.Sent()
	if len(snapshot) != 1 || snapshot[0] != message {
		t.Fatal("mailbox changed the rendered message")
	}
	snapshot[0].Body = "caller edit"
	if box.Sent()[0] != message {
		t.Fatal("editing a returned snapshot changed the retained message")
	}
	if strings.Contains(output.String(), message.Body) || !strings.Contains(output.String(), message.Subject) {
		t.Fatal("mailbox logging exposed the body or lost its delivery diagnostic")
	}
}

func TestConcurrentSendDoesNotLoseMessages(t *testing.T) {
	box := memory.New()
	var pending sync.WaitGroup
	for range 20 {
		pending.Go(func() {
			if err := box.Send(t.Context(), mail.Message{To: "recipient@example.test", Body: "body"}); err != nil {
				t.Error(err)
			}
			_ = box.Sent()
		})
	}
	pending.Wait()
	if len(box.Sent()) != 20 {
		t.Fatal("concurrent delivery lost a message")
	}
}
