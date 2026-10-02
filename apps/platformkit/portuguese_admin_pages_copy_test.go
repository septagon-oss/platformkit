package main

import (
	"net/http"
	"regexp"
	"testing"
)

// TestEveryPortugueseAdminPageWritesNoEnglishCopy: navigation, the skip link, the
// confirmation dialog, the session refusals the shell carries, list headings and
// a form's empty choice are all visible admin strings, so on a page answered in
// pt-PT none of them is the English source. The words chosen have a Portuguese
// translation distinct from the English one; "Menu", "Site" and "Email" are left
// out because Portuguese may write them the same.
func TestEveryPortugueseAdminPageWritesNoEnglishCopy(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	english := []string{
		"Skip to content", "Users", "Tenants", "Roles", "Tasks", "Billing",
		"Cancel", "Delete", "Sign-in required", "Permission denied", "Account changed",
		"Check the result", "Choose…", "Display name", "Created at", "Updated at",
	}
	for _, path := range []string{"/app", "/app/user/users", "/app/task/tasks/new", "/app/tenant/languages"} {
		code, _, _, page := r7navigate(t, cfg, admin, acmeHost, path, "pt-PT,pt;q=0.9")
		if code != http.StatusOK || pageLang(page) != "pt-PT" {
			t.Fatalf("%s answered %d with lang=%q, want a pt-PT page", path, code, pageLang(page))
		}
		for _, word := range english {
			if regexp.MustCompile(`>\s*` + regexp.QuoteMeta(word) + `\s*<`).MatchString(page) {
				t.Errorf("%s in pt-PT writes the English %q", path, word)
			}
		}
	}
}
