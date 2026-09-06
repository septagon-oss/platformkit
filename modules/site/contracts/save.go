package contracts

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
)

// Save is the decision behind Service.Save, with nothing under it: given what
// is stored — nil when the tenant has never saved — and what was sent, it is
// the settings as they are next and the event that announces them. It answers
// no event when the normalised body says what the stored row already says, so
// that " Acme " is "Acme" and a form submitted twice publishes once; it refuses
// what Validate refuses, as crud.ErrInvalid so both doors answer the same 422.
//
// at is the instant the command runs and fresh the id a first save gives the
// row: both are effects, and both arrive as arguments so that the decision is a
// function of what it is handed. internal applies it to Postgres and sitetest's
// fake to one value; the caller's copy of in is not touched.
func Save(stored *SiteSettings, in SiteSettings, at time.Time, fresh uuid.UUID) (crud.Outcome[SiteSettings], error) {
	in.Nav = slices.Clone(in.Nav) // Validate normalises the items in place
	if err := in.Validate(context.Background()); err != nil {
		return crud.Outcome[SiteSettings]{}, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	if stored == nil {
		// The tenant's first save: a row of its own, whatever the body claimed
		// about identity.
		in.Base = crud.Base{ID: fresh}
	} else {
		// An update of the one row there is: the id and the timestamps stay
		// the stored row's.
		in.Base = stored.Base
		if same(*stored, in) {
			return crud.Outcome[SiteSettings]{Next: *stored}, nil
		}
	}
	return crud.Outcome[SiteSettings]{Next: in, Event: EventSettingsUpdated, Payload: SettingsUpdated{
		SettingsID: in.ID, Title: in.Title, HomeSlug: in.HomeSlug, Theme: in.Theme, At: at,
	}}, nil
}

// same reports whether saving b over a would change anything a reader could
// see. It is field by field rather than a reflective comparison, so a field
// added to the entity and forgotten here is a save that publishes when it
// should have been silent — which is the harmless direction.
func same(a, b SiteSettings) bool {
	if a.Title != b.Title || a.Tagline != b.Tagline || a.HomeSlug != b.HomeSlug ||
		a.Theme != b.Theme || a.PrimaryColor != b.PrimaryColor {
		return false
	}
	if (a.LogoFileID == nil) != (b.LogoFileID == nil) ||
		(a.LogoFileID != nil && *a.LogoFileID != *b.LogoFileID) {
		return false
	}
	return slices.Equal(a.Nav, b.Nav)
}
