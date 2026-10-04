package main

import (
	"encoding/json"
	"net/http"
	"testing"

	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestMemberCanDiscoverItsAccountLanguageCommand(t *testing.T) {
	cfg, _ := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	const email = "language-member@acme.test"
	provisionAs(t, cfg, compose(cfg), acmeTenant(t, cfg), email, authcontracts.RoleMember)
	member := signIn(t, cfg, acmeHost, email, adminPass)
	code, body := do(t, cfg, member, http.MethodPost, acmeHost, "/api/v1/user/users/language", `{"locale":"pt-PT"}`)
	if code != http.StatusOK {
		t.Fatalf("member's language command = %d: %s", code, body)
	}
	if code, body := do(t, cfg, member, http.MethodGet, acmeHost, "/api/v1/user/users", ""); code != http.StatusForbidden {
		t.Fatalf("member should not read other users: %d: %s", code, body)
	}
	code, body = do(t, cfg, member, http.MethodGet, acmeHost, "/api/v1/app/resources", "")
	if code != http.StatusOK {
		t.Fatalf("catalog = %d: %s", code, body)
	}
	var catalog screens.Catalog
	if err := json.Unmarshal([]byte(body), &catalog); err != nil {
		t.Fatal(err)
	}
	for _, resource := range catalog.Resources {
		if resource.Module != "user" {
			continue
		}
		for _, command := range resource.Commands {
			if command.Verb == "language" && command.Self {
				return
			}
		}
	}
	t.Errorf("member's working account-language command is missing from its catalog: %s", body)
}
