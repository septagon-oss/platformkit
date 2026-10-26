package rest_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// TestASavingDeclarationNoSavingServiceCouldAnswerIsRefusedAtTheMount asks the
// boot hint gate one resource per case: the slug pair a declaration names is a
// contribution another module makes, so a pair that is not slugs, a declaration
// that names no field, and a word no screen can print are each refused at boot and
// named by the resource that declared them. The well-formed pair is served, and the
// entry that declared nothing is not asked at all.
func TestASavingDeclarationNoSavingServiceCouldAnswerIsRefusedAtTheMount(t *testing.T) {
	whole := func() *entity.SavingHints {
		return &entity.SavingHints{
			Module: "look", Kind: "outfit", KeyField: "id", VersionField: "revision",
			Words: entity.SavingWords{Save: "Save look", ListTitle: "My lookbook"},
		}
	}
	for _, tc := range []struct {
		name   string
		declar func() *entity.SavingHints
		want   []string
	}{
		{"a slug pair naming two fields", whole, nil},
		{"an entry that declared nothing", nil, nil},
		{"a pair that is not slugs", func() *entity.SavingHints {
			h := whole()
			h.Module = "My Looks"
			return h
		}, []string{"tasks/task", "savable", "My Looks/outfit", "slug"}},
		{"a declaration naming no key field", func() *entity.SavingHints {
			h := whole()
			h.KeyField = " "
			return h
		}, []string{"tasks/task", "savable", "key"}},
		{"a word over what a screen prints", func() *entity.SavingHints {
			h := whole()
			h.Words.Save = strings.Repeat("a", 300)
			return h
		}, []string{"tasks/task", "savable", "word"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := spec
			hints := entity.EntryHints{}
			if tc.declar != nil {
				hints.Saving = tc.declar()
			}
			s.Present = hints
			api, _ := patchDocumentAPI()
			s.Mount(api.Surfaces(s.Module))
			bad := rest.CheckRowHints(api.Resources())
			if tc.want == nil {
				if bad != "" {
					t.Fatalf("a declaration a saving service can answer is refused: %s", bad)
				}
				return
			}
			if bad == "" {
				t.Fatal("the boot gate served a saving declaration it cannot stand behind")
			}
			for _, w := range tc.want {
				if !strings.Contains(bad, w) {
					t.Errorf("the refusal %q does not name %s", bad, w)
				}
			}
		})
	}
}
