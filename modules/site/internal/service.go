// Package internal is every implementation of the site module. Nothing outside
// modules/site can import it, which is the compiler enforcing idea 3.
package internal

import (
	"context"
	"errors"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
)

// Service is the tenant's site settings. It has no fields: everything it needs
// arrives with the transaction it is given. The rule is not here either: Save
// is contracts.Save, applied to the one row.
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

// Save is contracts.Save, applied: the decision reads the stored row and the
// body, and this writes what it concluded — a first row, a changed row, or
// nothing — and publishes the event when there was a change. See
// contracts.Service.
func (s *Service) Save(ctx context.Context, tx db.Tx[db.Tenant], in *contracts.SiteSettings) (*contracts.SiteSettings, error) {
	stored, err := s.stored(tx)
	if err != nil {
		return nil, err
	}
	out, err := contracts.Save(stored, *in, tx.At(), db.NewID())
	if err != nil {
		return nil, err
	}
	next := &out.Next
	switch {
	case out.Event == "":
		return next, nil
	case stored == nil:
		// The unique index is what keeps it a singleton if two requests
		// arrive at once.
		err = crud.Create(ctx, tx, next)
	default:
		err = crud.Update(ctx, tx, next, columns...)
	}
	if err != nil {
		return nil, err
	}
	return next, events.Publish(ctx, tx, out.Event, out.Payload)
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
