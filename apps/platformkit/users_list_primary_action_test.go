package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// TestTheUsersListOffersOnePrimaryAction: the generated Users list has one thing to
// do per view (the design floor's "one primary call to action"). A person's own
// language belongs to their account menu, not to the administrator's list of people.
func TestTheUsersListOffersOnePrimaryAction(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	status, _, _, html := r7navigate(t, cfg, admin, acmeHost, "/app/user/users", "en")
	if status != http.StatusOK || !strings.Contains(html, `href="/app/user/users/new"`) {
		t.Fatalf("GET /app/user/users = %d, or the list offers no create link: %s", status, firstLineOf(html))
	}
	main := html[strings.Index(html, "<main"):]
	if n := len(regexp.MustCompile(`data-variant="primary"`).FindAllString(main, -1)); n != 1 {
		t.Errorf("the Users list carries %d primary actions, want 1", n)
	}
}
