package pkit_test

// The first line of the composition file counts the modules the app builds, and
// a client commits that line as COMPOSITION.<env>.md (decision 0074 rule 4). It
// is the one line whose subject is a number, so it is the one line that has to
// agree with it: "collect in development builds 1 module.", not "builds 1
// modules." — the wording a one-module pilot, which is what 0074 asks a client
// to commit first, would carry into its own documentation.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/cart"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/user"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/wishlist"
)

func TestExplainCountsTheModulesItBuilds(t *testing.T) {
	for _, want := range []struct {
		modules []*pkit.Module
		line    string
	}{
		{[]*pkit.Module{cart.Module}, "pkit: collect in development builds 1 module."},
		{[]*pkit.Module{user.Module, wishlist.Module}, "pkit: collect in development builds 2 modules."},
	} {
		app := pkit.NewApp("collect").Use(want.modules...)
		text, err := app.Explain(dev)
		if err != nil {
			t.Fatalf("%d modules was refused: %v", len(want.modules), err)
		}
		head := strings.SplitN(text, "\n", 2)[0]
		if head != want.line {
			t.Errorf("the composition file counts %d modules as %q, want %q", len(want.modules), head, want.line)
		}
	}
}
