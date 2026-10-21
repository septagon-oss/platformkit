package main

import (
	"encoding/json"
	"net/http"
	"testing"

	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestACommandOnlyCatalogueViewLeavesOtherReadersRowsAndLanguageIntact(t *testing.T) {
	cfg, _ := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	const email = "catalogue-reader@acme.test"
	provisionAs(t, cfg, compose(cfg), acmeTenant(t, cfg), email, authcontracts.RoleMember)
	member := signIn(t, cfg, acmeHost, email, adminPass)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	for _, caller := range []struct {
		name     string
		client   *http.Client
		readable bool
		language string
		one      string
	}{
		{"member first", member, false, "pt-PT", "pessoa"},
		{"administrator", admin, true, "en", "person"},
		{"member again", member, false, "pt-PT", "pessoa"},
	} {
		t.Run(caller.name, func(t *testing.T) {
			code, _, body := catalogueDocument(t, cfg, caller.client, caller.language)
			if code != http.StatusOK {
				t.Fatalf("catalogue = %d", code)
			}
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			var document screens.Catalog
			if err := json.Unmarshal(raw, &document); err != nil {
				t.Fatal(err)
			}
			for _, entry := range document.Resources {
				if entry.Module != "user" || entry.Entity != "user" {
					continue
				}
				if entry.Presentation == nil || entry.Presentation.Singular == nil || *entry.Presentation.Singular != caller.one {
					t.Errorf("people presentation in %s must have singular %q: %s", caller.language, caller.one, raw)
				}
				if caller.readable {
					if len(entry.Fields) == 0 || entry.Screen == "" || !entry.Writable || entry.Operations != nil {
						t.Errorf("the administrator lost the rows or operations after the member's read: %+v", entry)
					}
				} else if len(entry.Fields) != 0 || entry.Screen != "" || entry.Writable || entry.WritePath != "" || len(entry.Immutable) != 0 || entry.Operations == nil || len(entry.Operations) != 0 {
					t.Errorf("the member's command-only entry describes rows or offers an operation (operations omitted=%t): %+v", entry.Operations == nil, entry)
				}
				for _, command := range entry.Commands {
					if command.Verb == "language" && command.Self {
						return
					}
				}
				t.Fatal("the account-language command disappeared")
			}
			t.Fatal("the people resource disappeared")
		})
	}
}
