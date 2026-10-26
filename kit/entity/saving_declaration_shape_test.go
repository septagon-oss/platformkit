package entity_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// savable is one declaration in the shape every case below starts from: a slug
// pair, the two fields of the read it names, and four printed words.
func savable() *entity.SavingHints {
	return &entity.SavingHints{
		Module: "look", Kind: "outfit", KeyField: "id", VersionField: "revision",
		Words: entity.SavingWords{
			Save: "Save look", Saved: "Saved to lookbook",
			Remove: "Remove from lookbook", ListTitle: "My lookbook",
		},
	}
}

func TestSavingValidAcceptsASlugPairNamingTwoFieldsAndFourWords(t *testing.T) {
	t.Parallel()
	if err := entity.SavingValid(savable()); err != nil {
		t.Fatalf("SavingValid(whole declaration) = %v, want nil", err)
	}
	// An empty word is a word the author left to the fallback, not a bad one.
	bare := savable()
	bare.Words = entity.SavingWords{}
	if err := entity.SavingValid(bare); err != nil {
		t.Fatalf("SavingValid(no words) = %v, want nil: silence asks for the fallback", err)
	}
}

func TestSavingValidRefusesTheDeclarationNoScreenCouldDraw(t *testing.T) {
	t.Parallel()
	tooLong := strings.Repeat("a", 257)
	for name, edit := range map[string]func(*entity.SavingHints){
		"module is not a slug":   func(h *entity.SavingHints) { h.Module = "My Looks" },
		"kind is not a slug":     func(h *entity.SavingHints) { h.Kind = "" },
		"key field is blank":     func(h *entity.SavingHints) { h.KeyField = "   " },
		"version field is blank": func(h *entity.SavingHints) { h.VersionField = "" },
		"a word is over a line":  func(h *entity.SavingHints) { h.Words.Save = tooLong },
		"a word carries a control character": func(h *entity.SavingHints) {
			h.Words.ListTitle = "My\nlookbook"
		},
		"a word is only whitespace": func(h *entity.SavingHints) { h.Words.Saved = " \t" },
	} {
		t.Run(name, func(t *testing.T) {
			h := savable()
			edit(h)
			if err := entity.SavingValid(h); err == nil {
				t.Fatalf("SavingValid(%s) = nil, want a refusal naming the pair", name)
			}
		})
	}
}

func TestSavingValidRefusesTheAbsenceRatherThanGuessingAtOne(t *testing.T) {
	t.Parallel()
	if err := entity.SavingValid(nil); err == nil {
		t.Fatal("SavingValid(nil) = nil, want a refusal: there is no declaration to check")
	}
}

func TestValidSavingSlugHoldsTheGrammarAContributionIsRefusedOver(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"look", "a", "look_outfit2", "x0"} {
		if !entity.ValidSavingSlug(ok) {
			t.Errorf("ValidSavingSlug(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "Look", "2look", "look-outfit", "look outfit", "look/outfit"} {
		if entity.ValidSavingSlug(bad) {
			t.Errorf("ValidSavingSlug(%q) = true, want false", bad)
		}
	}
}
