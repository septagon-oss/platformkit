package pkit_test

// Decision 0074 rule 3 says a product contributes its landing, that the module
// which takes one landing "refuses two where it accepts one, with Choose as the
// fix", and that the ambiguity sentence ends "Choose in collect". The refusal
// is asserted in compose_test.go; this asserts the other half — that doing what
// the sentence says settles the composition, picks the one contribution, and
// withdraws the module that lost.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/blog"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/homepage"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/shop"
)

func TestChooseSettlesTwoContributionsOntoTheModuleThatTakesOne(t *testing.T) {
	app := pkit.NewApp("collect").Use(homepage.Module, shop.Module, blog.Module).Choose(blog.Module)

	if err := app.Validate(dev); err != nil {
		t.Fatalf("doing what the refusal said still fails: %v", err)
	}

	text, err := app.Explain(dev)
	if err != nil {
		t.Fatalf("Explain refused the composition Validate accepted: %v", err)
	}
	for _, want := range []string{
		"pkit: homepage.Module needs homepagecontracts.Landing from blog.Module.",
		"pkit: collect chose blog.Module over shop.Module for homepagecontracts.Landing.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the composition file does not read %q:\n%s", want, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "pkit: shop.Module") {
			t.Errorf("the module the app chose against is still in the composition file: %q\n%s", line, text)
		}
	}
}
