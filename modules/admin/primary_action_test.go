package admin_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/admin"
)

// TestTheHomeServesTheComposedPrimaryAction is the port's whole path through a
// mounted shell: the action Deps carries is drawn on /app before the first card
// for a caller who may follow it, and is not drawn at all for one who may not —
// the page answering 200 either way.
func TestTheHomeServesTheComposedPrimaryAction(t *testing.T) {
	write := func(d *admin.Deps) {
		d.Primary = module.NavEntry{Label: "Write a note", Screen: "note/notes", Permission: "note:read"}
	}
	button := regexp.MustCompile(`<a [^>]*data-component="button"[^>]*data-variant="primary"[^>]*href="/app/note/notes"[^>]*>`)

	code, body, _ := call(t, mountAs(t, member{"note:read": true}, write), http.MethodGet, "/app", "")
	if code != http.StatusOK {
		t.Fatalf("the home = %d %s", code, body)
	}
	main := regexp.MustCompile(`(?s)<main\b.*?</main>`).FindString(body)
	at := button.FindStringIndex(main)
	if at == nil || !strings.Contains(main[at[0]:], "Write a note<") {
		t.Fatalf("the home does not draw the composed action for a caller who may follow it:\n%s", main)
	}
	if card := strings.Index(main, `data-component="card"`); card < 0 || card < at[0] {
		t.Errorf("a card comes at %d, before the action at %d (or there is none)", card, at[0])
	}

	code, body, _ = call(t, mountAs(t, member{}, write), http.MethodGet, "/app", "")
	if code != http.StatusOK {
		t.Fatalf("the home for a caller holding nothing = %d %s", code, body)
	}
	if button.MatchString(body) {
		t.Errorf("the action is drawn for a caller who may not follow it:\n%s", body)
	}
}
