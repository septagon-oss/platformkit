package translationtest

import (
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func reviewMachineDraft(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	f.WithMachine(func(text, from, to string) (string, error) { return "Máquina: " + text, nil })
	f.once(t, "the suggestion", func() {
		if err := f.suggest(t, id); err != nil {
			t.Fatalf("Suggest: %v", err)
		}
	})
	workspace := f.read(t, id, false)
	if workspace[FieldBody].Status != rest.FallbackMachine {
		t.Errorf("the workspace sees %q, where a labelled draft belongs", workspace[FieldBody].Status)
	}
	public := f.read(t, id, true)
	if public[FieldBody].Status != rest.FallbackWithheld {
		t.Errorf("the public door sees %q, want %q", public[FieldBody].Status, rest.FallbackWithheld)
	}
	if strings.HasPrefix(public[FieldBody].Value, "Máquina") {
		t.Errorf("the public door served the machine draft: %q", public[FieldBody].Value)
	}
	f.once(t, "the review", func() {
		if err := f.review(t, id); err != nil {
			t.Fatalf("Review: %v", err)
		}
	})
	after := f.read(t, id, true)
	if after[FieldBody].Status != "" || !strings.HasPrefix(after[FieldBody].Value, "Máquina") {
		t.Errorf("after review the public door reports %q / %q, want the text and nothing to say",
			after[FieldBody].Status, after[FieldBody].Value)
	}
}

func refuseUnavailableMachine(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	f.silent(t, "a suggestion with no provider", func() {
		if err := f.suggest(t, id); !errors.Is(err, crud.ErrInvalid) {
			t.Fatalf("Suggest with no translator answered %v, want the refusal that names the setting", err)
		}
	})
	if rows := f.Rows(); len(rows) != 0 {
		t.Errorf("a refusal wrote %d rows", len(rows))
	}

	f.WithMachine(func(text, from, to string) (string, error) { return "Rascunho: " + text, nil })
	if err := f.suggest(t, id); err != nil {
		t.Fatalf("the first Suggest: %v", err)
	}
	f.WithMachine(func(text, from, to string) (string, error) { return text, nil })
	f.silent(t, "the echoing suggestion", func() {
		if err := f.suggest(t, id); err == nil {
			t.Fatal("a provider that handed back the English was accepted as Portuguese")
		}
	})
	f.WithMachine(func(text, from, to string) (string, error) { return "Segundo: " + text, nil })
	if err := f.suggestAt(t, id, 1); err != nil {
		t.Fatalf("the second Suggest: %v", err)
	}
	var revisions []int64
	for _, r := range f.Rows() {
		if r.Field == FieldBody {
			revisions = append(revisions, r.Revision)
		}
	}
	if len(revisions) != 1 || revisions[0] != 2 {
		t.Errorf("two drafts of one field left revisions %v, want one row at revision 2", revisions)
	}
	if got := f.read(t, id, false)[FieldBody].Status; got != rest.FallbackMachine {
		t.Errorf("the second draft reports %q; a draft is unreviewed until somebody says otherwise", got)
	}
}

func refuseOverlongDraft(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	f.WithMachine(func(text, from, to string) (string, error) { return overLong, nil })
	f.silent(t, "the over-long draft", func() {
		if err := f.suggestTitle(t, id); !errors.Is(err, crud.ErrInvalid) {
			t.Fatalf("a draft over the field's ceiling = %v, want the field's own rule to refuse it", err)
		}
	})
	if rows := f.Rows(); len(rows) != 0 {
		t.Fatalf("a draft over the field's ceiling committed %d rows; a refused write leaves none to review", len(rows))
	}
	if err := f.suggest(t, id); err != nil {
		t.Fatalf("the same text under a field with no ceiling = %v, want a saved draft", err)
	}
}
