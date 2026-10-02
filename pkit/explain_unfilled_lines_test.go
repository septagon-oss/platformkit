package pkit_test

// Explain is the text a client commits as COMPOSITION.<env>.md (decision 0074
// rule 4), so every line it prints has to say who is involved. A contribution
// no module takes and a taker with no contributor both print a line naming
// nobody: "… contributes one homepagecontracts.Landing to ." and "… takes
// every cartcontracts.Extension from ." The delivery may answer either case by
// naming the modules or by plainly saying no module is involved; naming none
// with a bare preposition is neither.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/blog"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/cart"
)

// namesSomeone reports whether a line says who is on the other end, either by
// naming a module or by plainly saying nobody is.
func namesSomeone(line string) bool {
	return strings.HasSuffix(line, "Module.") ||
		strings.Contains(line, "no module") ||
		strings.Contains(line, "no composed module")
}

func TestExplainSaysWhoTakesAContribution(t *testing.T) {
	app := pkit.NewApp("collect").Use(blog.Module)
	text, err := app.Explain(dev)
	if err != nil {
		// Refusing a contribution nobody takes answers it honestly too.
		if !strings.Contains(err.Error(), "blog") {
			t.Errorf("the refusal does not name the module: %v", err)
		}
		return
	}
	t.Logf("Explain of a contribution nobody takes:\n%s", text)
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "contributes one homepagecontracts.Landing") {
			continue
		}
		if !namesSomeone(line) {
			t.Errorf("the contribution line names nobody: %q", line)
		}
	}
}

func TestExplainSaysWhoContributesWhatATakerTakes(t *testing.T) {
	app := pkit.NewApp("collect").Use(cart.Module)
	text, err := app.Explain(dev)
	if err != nil {
		t.Fatalf("a taker with no contributor is honest (0074: zero or more): %v", err)
	}
	t.Logf("Explain of a taker with no contributor:\n%s", text)
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "cart.Module takes every cartcontracts.Extension") {
			continue
		}
		if !namesSomeone(line) {
			t.Errorf("the line naming who contributes says nobody: %q", line)
		}
	}
}
