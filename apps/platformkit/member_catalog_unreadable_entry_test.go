package main

import (
	"encoding/json"
	"net/http"
	"testing"

	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// A member may call their own account-language command on the people resource and
// may read none of its rows, so their catalogue names the resource for the command's
// sake. The catalogue's Entry contract is that an unreadable resource is listed with
// empty operations — what a caller may not look at, they are not offered — and an
// absent operations key means all five. A shell that reads this member's document
// must therefore find the command and no list, read or write to draw.
func TestAMembersCatalogueOffersNoOperationOnPeopleItMayNotRead(t *testing.T) {
	cfg, _ := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	const email = "catalogue-member@acme.test"
	provisionAs(t, cfg, compose(cfg), acmeTenant(t, cfg), email, authcontracts.RoleMember)
	member := signIn(t, cfg, acmeHost, email, adminPass)
	if code, body := do(t, cfg, member, http.MethodGet, acmeHost, "/api/v1/user/users", ""); code != http.StatusForbidden {
		t.Fatalf("a member's read of the people list = %d, want 403: %s", code, body)
	}
	code, body := do(t, cfg, member, http.MethodGet, acmeHost, "/api/v1/app/resources", "")
	if code != http.StatusOK {
		t.Fatalf("catalogue = %d: %s", code, body)
	}
	var document struct {
		Resources []struct {
			Module     string    `json:"module"`
			Entity     string    `json:"entity"`
			Operations *[]string `json:"operations"`
			Commands   []struct {
				Verb string `json:"verb"`
				Self bool   `json:"self"`
			} `json:"commands"`
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(body), &document); err != nil {
		t.Fatal(err)
	}
	for _, e := range document.Resources {
		if e.Module != "user" || e.Entity != "user" {
			continue
		}
		self := false
		for _, c := range e.Commands {
			self = self || (c.Verb == "language" && c.Self)
		}
		if !self {
			t.Fatalf("the member's people entry carries no account-language command: %s", body)
		}
		if e.Operations == nil {
			t.Fatalf("the member's people entry prints no operations key, which offers all five to a member refused the list: %s", body)
		}
		if len(*e.Operations) != 0 {
			t.Errorf("the member's people entry offers %v to a member refused the list", *e.Operations)
		}
		return
	}
	t.Fatalf("the member's catalogue names no people entry for its account-language command: %s", body)
}
