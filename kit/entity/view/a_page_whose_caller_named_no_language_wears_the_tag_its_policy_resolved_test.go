package view_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAPageWhoseCallerNamedNoLanguageWearsTheTagItsPolicyResolved asks which of the
// two tags a page shows in Page.Language. State.Language "" is documented as no tag
// of its own, so the page answers with the policy's resolved tag rather than with
// nothing: a shell that reads an empty language cannot pick a translation. The
// converse is asked too, because the two arguments are otherwise interchangeable —
// refuse forbids a caller and a policy naming two different tags, so the only case
// that tells them apart is the one where a caller names a tag and the policy names
// none. Both sentences also ask that a tag the two agree on still arrives, so neither
// can pass by writing the other argument unconditionally.
func TestAPageWhoseCallerNamedNoLanguageWearsTheTagItsPolicyResolved(t *testing.T) {
	t.Parallel()

	rows := listInput().Data.Records

	noTagOfItsOwn := input(view.KindList, rows, func(s *view.State) { s.Language = "" })
	noTagOfItsOwn.Policy.Language = "pt"
	if got := build(t, noTagOfItsOwn).Language; got != "pt" {
		t.Errorf("a list page whose caller named no language reads language %q, want the policy's resolved tag %q", got, "pt")
	}

	policyWithoutTag := input(view.KindList, rows, func(s *view.State) { s.Language = "pt" })
	policyWithoutTag.Policy.Language = ""
	if got := build(t, policyWithoutTag).Language; got != "pt" {
		t.Errorf("a list page over a policy that resolved no tag reads language %q, want the %q its caller asked for", got, "pt")
	}

	// The tag the two agree on is the pair a build accepts when both name one, and the
	// pair every fixture is built from, so it is asked here for the same reason.
	agreed := input(view.KindList, rows, func(s *view.State) { s.Language = "pt" })
	agreed.Policy.Language = "pt"
	if got := build(t, agreed).Language; got != "pt" {
		t.Errorf("a list page whose caller and policy both name %q reads language %q, want the tag they named", "pt", got)
	}
}
