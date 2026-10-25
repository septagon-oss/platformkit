package auth_test

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	contracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// TestEveryInvitationReadingFieldTheREADMEStatesIsCarried checks the reading's own
// field table against the shape the two reading routes answer with. The table is
// what the People interface builds against, so a row naming a field the reading
// does not carry promises an interface a caller can never read.
func TestEveryInvitationReadingFieldTheREADMEStatesIsCarried(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	const heading = "## Invitation credentials and readings"
	section := string(readme)[strings.Index(string(readme), heading):]
	section = section[:strings.Index(section, "\n### ")]
	carried := invitationReadingFields(t)
	for _, named := range statedReadingFields(t, section) {
		if _, ok := carried[named]; !ok {
			t.Errorf("the auth README states the invitation reading carries %q, which no reading route answers with; the reading carries %s",
				named, strings.Join(keysOf(carried), ", "))
		}
	}
}

// invitationReadingFields marshals one empty reading and returns every JSON field
// name it carries, including the snapshot the reading embeds.
func invitationReadingFields(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	body, err := json.Marshal(&contracts.InvitationReading{InvitationSnapshot: &user.InvitationSnapshot{}})
	if err != nil {
		t.Fatal(err)
	}
	var carried map[string]json.RawMessage
	if err := json.Unmarshal(body, &carried); err != nil {
		t.Fatal(err)
	}
	return carried
}

// statedReadingFields reads the field names of the reading's markdown table: the
// first cell of every row that names a field in backticks.
func statedReadingFields(t *testing.T, section string) []string {
	t.Helper()
	row := regexp.MustCompile("(?m)^\\| `([A-Za-z][A-Za-z0-9]*)` \\|")
	var names []string
	for _, match := range row.FindAllStringSubmatch(section, -1) {
		names = append(names, match[1])
	}
	if len(names) == 0 {
		t.Fatalf("the reading's section of the auth README states no field table in %q", section)
	}
	return names
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
