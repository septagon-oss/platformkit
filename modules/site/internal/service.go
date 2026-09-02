// Package internal is every implementation of the site module. Nothing outside
// modules/site can import it, which is the compiler enforcing idea 3.
package internal

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
)

// Service is the tenant's site settings. It has no fields: everything it needs
// arrives with the transaction it is given.
type Service struct{}

// NewService returns the two operations. module.go constructs it.
func NewService() *Service { return &Service{} }

var _ contracts.Service = (*Service)(nil)

// Settings is what this tenant has configured, or the defaults. See
// contracts.Service.
func (s *Service) Settings(ctx context.Context, tx db.Tx[db.Tenant]) (*contracts.SiteSettings, error) {
	stored, err := s.stored(tx)
	if err != nil {
		return nil, err
	}
	if stored != nil {
		return stored, nil
	}
	// Not a row, and not an error either: every tenant has a site from the
	// moment it exists, and this is what it says before anybody has said
	// anything. Validate fills in the theme and the colour.
	out := &contracts.SiteSettings{}
	return out, out.Validate(ctx)
}

// Save writes the settings and says so, unless nothing changed. See
// contracts.Service.
func (s *Service) Save(ctx context.Context, tx db.Tx[db.Tenant], in *contracts.SiteSettings) (*contracts.SiteSettings, error) {
	stored, err := s.stored(tx)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		// The tenant's first save. crud.Create validates, stamps the tenant and
		// gives the row its id; the unique index is what keeps it a singleton
		// if two requests arrive at once.
		crud.Reset(in)
		if err := crud.Create(ctx, tx, in); err != nil {
			return nil, err
		}
		return in, publish(ctx, tx, in)
	}
	// The id and the timestamps stay the stored row's: this is an update of the
	// one row there is, whatever the body claimed about identity.
	in.Base = stored.Base
	// Validated here as well as by crud.Update below, because the comparison
	// that decides whether anything changed has to run against the normalised
	// values: " Acme " and "Acme" are the same title, and a save that published
	// because of the whitespace would be a cache invalidated for nothing. The
	// wrapping is kit/crud's, so both doors answer with the same 422.
	if err := in.Validate(ctx); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	if same(stored, in) {
		return stored, nil
	}
	if err := crud.Update(ctx, tx, in, columns...); err != nil {
		return nil, err
	}
	return in, publish(ctx, tx, in)
}

// columns are the seven a save writes, and the stamp. They are written out
// rather than left to a whole-row update so that a column added later has to be
// added here too — the alternative is a field nobody can save and nobody
// notices.
var columns = []string{"title", "tagline", "home_slug", "theme", "primary_color", "logo_file_id", "nav", "updated_at"}

// stored is the tenant's row, or nil when it has none.
func (s *Service) stored(tx db.Tx[db.Tenant]) (*contracts.SiteSettings, error) {
	var row contracts.SiteSettings
	err := tx.DB().Where("deleted_at IS NULL").Take(&row).Error
	switch classified := crud.Classify(err); {
	case classified == nil:
		return &row, nil
	case errors.Is(classified, crud.ErrNotFound):
		return nil, nil
	default:
		return nil, classified
	}
}

// same reports whether saving in would change anything a reader could see. It
// is field by field rather than a reflective comparison, so a field added to
// the entity and forgotten here is a save that publishes when it should have
// been silent — which is the harmless direction.
func same(a, b *contracts.SiteSettings) bool {
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

func publish(ctx context.Context, tx db.Tx[db.Tenant], s *contracts.SiteSettings) error {
	return events.Publish(ctx, tx, contracts.EventSettingsUpdated, contracts.SettingsUpdated{
		SettingsID: s.ID, Title: s.Title, HomeSlug: s.HomeSlug, Theme: s.Theme, At: db.Now(),
	})
}
