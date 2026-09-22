package fault_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// capability is how a guide names another module's value package: "auth/contracts",
// "calendar/contracts". The prefix is what has to exist in this repository.
var capability = regexp.MustCompile(`([a-z][a-z0-9_-]*)/(contracts|events|domain)`)

// TestAGuideNamesOnlyAModuleThisRepositoryHolds is the reviewer's pin for the
// rule ADR 0009 states and T-0031's brief repeats: public documentation
// describes the consumer seam without naming catalog capabilities. A guide may
// say "a module's value package"; naming a package that lives in a repository
// this one does not publish is a disclosure the foundation cannot undo. The
// check is mechanical rather than a taste judgement: every "<capability>/…" a
// kit guide names must name a module this tree holds, which is what an external
// consumer can read and no more. Repository-owned prefixes are skipped because
// they name a directory here, not a capability elsewhere.
func TestAGuideNamesOnlyAModuleThisRepositoryHolds(t *testing.T) {
	guides, err := filepath.Glob("../../kit/*/README.md")
	if err != nil {
		t.Fatalf("kit guides: %v", err)
	}
	if len(guides) == 0 {
		t.Fatal("no kit guide was read, so this check proves nothing")
	}
	for _, guide := range guides {
		text, err := os.ReadFile(guide)
		if err != nil {
			t.Fatalf("read %s: %v", guide, err)
		}
		for _, match := range capability.FindAllStringSubmatch(string(text), -1) {
			switch owner := match[1]; owner {
			case "kit", "ui", "design", "modules", "apps", "tools":
			default:
				if _, err := os.Stat(filepath.Join("../../modules", owner)); err != nil {
					t.Errorf("%s names %q, a capability this repository does not hold: ADR 0009 keeps a private catalog's names out of a public guide",
						filepath.Base(filepath.Dir(guide)), match[0])
				}
			}
		}
	}
}
