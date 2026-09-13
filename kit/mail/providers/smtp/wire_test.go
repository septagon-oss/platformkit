package smtp

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/mail"
)

func TestRenderedMailFoldsSubjectAndKeepsPlainTextBody(t *testing.T) {
	got := wire("sender@example.test", mail.Message{
		To: "recipient@example.test", Subject: "Account setup\r\nBcc: other@example.test",
		Body: "first\r\nsecond\n<script>plain text</script>",
	})
	head, body, ok := strings.Cut(got, "\r\n\r\n")
	if !ok || strings.Contains(head, "\r\nBcc:") || !strings.Contains(head, "Content-Type: text/plain; charset=utf-8") {
		t.Fatal("mail headers accepted an injected line or changed the content type")
	}
	if body != "first\r\nsecond\r\n<script>plain text</script>" {
		t.Fatalf("plain-text body changed: %q", body)
	}
}
