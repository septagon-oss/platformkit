package contracts_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
)

// TestSaveDecidesFromWhatItIsHanded is the rule with no database under it: a
// first save is a fresh row, a body that says what is stored — whitespace and
// all — is silence, a change is announced, a refusal is the caller's 422, and
// the caller's copy is left alone.
func TestSaveDecidesFromWhatItIsHanded(t *testing.T) {
	at, fresh := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC), uuid.New()
	sent := contracts.SiteSettings{Title: " Acme ", Nav: contracts.Nav{{Label: " About ", Path: "/about-us"}}}

	first, err := contracts.Save(nil, sent, at, fresh)
	if err != nil || first.Event != contracts.EventSettingsUpdated || first.Next.ID != fresh || first.Next.Title != "Acme" {
		t.Fatalf("the first save = %+v, %v; want a row with the fresh id, normalised and announced", first, err)
	}
	if sent.Nav[0].Label != " About " {
		t.Errorf("Save trimmed the caller's navigation to %q; a decision does not act on its arguments", sent.Nav[0].Label)
	}
	stored := first.Next
	if again, err := contracts.Save(&stored, sent, at.Add(time.Hour), uuid.New()); err != nil || again.Event != "" || again.Next.ID != fresh {
		t.Errorf("the same form again = %+v, %v; want silence and the stored row", again, err)
	}
	changed := sent
	changed.Tagline = "We make things"
	if out, err := contracts.Save(&stored, changed, at, uuid.New()); err != nil || out.Event == "" || out.Next.ID != fresh {
		t.Errorf("a change = %+v, %v; want it announced on the one row there is", out, err)
	}
	if _, err := contracts.Save(&stored, contracts.SiteSettings{PrimaryColor: "blue"}, at, fresh); !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("a colour that is not one = %v; want ErrInvalid", err)
	}
}
