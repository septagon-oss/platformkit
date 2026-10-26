package internal_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// The browser journey must recognize the message the worker actually sends.
// Read its expression instead of maintaining a second copy of its path rule.
func TestMailedVerificationLinkIsRecognizedByTheBrowserJourney(t *testing.T) {
	source, err := os.ReadFile("../../../e2e/mailed-links.spec.ts")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(source), "const linkPattern = /")
	if !ok {
		t.Fatal("the mailed-link journey no longer exposes its link expression; connect this case to its new reader")
	}
	expression, _, ok := strings.Cut(rest, "/g;")
	if !ok {
		t.Fatal("cannot read the mailed-link journey's complete expression")
	}
	// JavaScript escapes its slash delimiter; Go's regexp has no delimiter.
	reader, err := regexp.Compile(strings.ReplaceAll(expression, `\/`, `/`))
	if err != nil {
		t.Fatalf("read the journey's link expression: %v", err)
	}
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
	verificationSignup(t, conn, router, "journey-link@example.com")
	letters := mailbox.Sent()
	if len(letters) != 1 {
		t.Fatalf("worker mailed %d messages, want one", len(letters))
	}
	if links := reader.FindAllString(letters[0].Body, -1); len(links) != 1 {
		t.Errorf("the browser journey recognizes %d verification links in the worker's message, want one; its expression is %s", len(links), expression)
	}
}
