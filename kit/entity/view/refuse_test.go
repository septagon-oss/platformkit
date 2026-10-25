package view_test

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAPolicyVersionThisBuildCannotReadIsRefused is the version rule: a Page built
// from a policy this build cannot read would be the double derivation the contract
// exists to end, so the answer is no Page rather than a half-understood one.
func TestAPolicyVersionThisBuildCannotReadIsRefused(t *testing.T) {
	t.Parallel()
	for _, version := range []int{0, 2, present.PolicyVersion + 7} {
		in := listInput()
		in.Policy.PolicyVersion = version
		page, err := view.Build(in)
		if err == nil {
			t.Fatalf("a policy at revision %d was built", version)
		}
		if wroteNothing(page) == false {
			t.Errorf("a refused build returned a page: %+v", page)
		}
		if !errors.Is(err, view.Refusal{Field: "policyVersion"}) {
			t.Errorf("refusal %v does not name the policy's own revision", err)
		}
	}
}

func TestARefusalNamesTheMemberItRefuses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*view.Input)
		field  string
	}{
		{"an unknown surface", func(in *view.Input) { in.State.Kind = "deck" }, "kind"},
		{"two languages on one page", func(in *view.Input) { in.State.Language = "pt-PT" }, "language"},
		{"no instant to anchor a phrase", func(in *view.Input) { in.State.Now = zeroTime() }, "builtAt"},
		{"no page size", func(in *view.Input) { in.Data.Window.Limit = 0 }, "window.limit"},
		{"a negative count with the count asked for", func(in *view.Input) { in.Data.Total = -1 }, "total"},
		{"a resource named neither module/entity nor nothing", func(in *view.Input) { in.Resource.Resource = "task" }, "resource"},
		{"a list the resource does not answer", func(in *view.Input) { in.Resource.Operations = []string{"read"} }, "operations"},
		{"a form for a caller who may not write", func(in *view.Input) {
			in.State.Kind = view.KindForm
			in.State.Writable = false
		}, "writable"},
		{"a create form for a singleton", func(in *view.Input) {
			in.State.Kind = view.KindForm
			in.State.Form.Create = true
			in.Resource.Singleton = true
		}, "operations"},
		{"an update form the resource does not answer", func(in *view.Input) {
			in.State.Kind = view.KindForm
			in.Resource.Operations = []string{"list", "read", "create"}
		}, "operations"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := listInput()
			tc.mutate(&in)
			page, err := view.Build(in)
			if err == nil {
				t.Fatalf("no refusal: %+v", page)
			}
			var refusal view.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("%v is not a Refusal", err)
			}
			if refusal.Field != tc.field {
				t.Errorf("refusal names %q, want %q", refusal.Field, tc.field)
			}
			if wroteNothing(page) == false {
				t.Errorf("a refused build wrote a page: %+v", page)
			}
			if !errors.Is(err, view.Refusal{Field: tc.field}) {
				t.Errorf("errors.Is does not find the member of %v", err)
			}
		})
	}
}

func TestARecordPageFromNoRowIsRefused(t *testing.T) {
	t.Parallel()
	in := input(view.KindRecord, nil, nil)
	if _, err := view.Build(in); !errors.Is(err, view.Refusal{Field: "record"}) {
		t.Fatalf("err %v: a record page from no row would be a fiction", err)
	}
}

func TestAListPageOfNothingIsNotARefusal(t *testing.T) {
	t.Parallel()
	in := listInput()
	in.Data.Records = nil
	in.Data.Total = 0
	in.Data.Counted = false
	if _, err := view.Build(in); err != nil {
		t.Fatalf("an empty collection refused: %v", err)
	}
}

func TestACommandOnlyResourceAnswersNoPage(t *testing.T) {
	t.Parallel()
	s := taskSchema()
	in := listInput()
	in.Resource.Operations = []string{"read"}
	in.Policy = present.Resolve(present.Input{
		Schema: s, CommandsOnly: true, Operations: []string{"read"},
		Commands: []present.CommandInput{commandInput("archive")}, Language: "en",
	})
	if _, err := view.Build(in); !errors.Is(err, view.Refusal{Field: "operations"}) {
		t.Fatalf("err %v: a resource with no list, no record and no form has no page", err)
	}
	// What it offers still reaches a person: as the actions of another page.
	other := in
	other.State.Kind = view.KindRecord
	other.Resource.Commands = []view.CommandDescription{{Verb: "archive"}}
	other.Data.Records = []map[string]any{{"id": "t1"}}
	if actions := build(t, other).Actions; len(actions) == 0 {
		t.Error("a commands-only resource's commands reached no page at all")
	}
}

func TestAPageOfAnUnregisteredResourceIsAllowed(t *testing.T) {
	t.Parallel()
	in := listInput()
	in.Resource.Resource = ""
	in.State.Kind = view.KindRecord
	in.Data.Records = []map[string]any{{"id": "t1", "title": "n"}}
	page := build(t, in)
	if page.Ref != (view.Ref{ID: "t1"}) {
		t.Errorf("ref %+v, want the row's id named and no resource claimed", page.Ref)
	}
	if len(page.Breadcrumbs) != 2 || page.Breadcrumbs[0].Label != "Workspace" || page.Breadcrumbs[0].Ref != (view.Ref{}) {
		t.Errorf("breadcrumbs %+v, want the caller's root and the record, with no collection between them", page.Breadcrumbs)
	}
}
