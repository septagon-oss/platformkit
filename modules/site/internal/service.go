// Package internal is every implementation of the site module. Nothing outside
// modules/site can import it, which is the compiler enforcing idea 3.
package internal

import (
	"context"
	"errors"
	"strings"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
	"gorm.io/gorm/clause"
)

// Service is the tenant's site settings. It holds the one thing a read cannot
// get from the transaction it is given: the installation's own accent, for the
// site that chose none.
type Service struct {
	palette contracts.Palette
}

// NewService returns the two operations. module.go constructs it, once, with the
// palette the composition named for itself; a nil palette is an installation with
// no accent of its own, and a site that chose none is then answered the empty
// colour and pinned nowhere.
func NewService(palette contracts.Palette) *Service { return &Service{palette: palette} }

var (
	_ contracts.Service      = (*Service)(nil)
	_ contracts.LockedReader = (*Service)(nil)
)

// Settings is what this tenant has configured, or the defaults. See
// contracts.Service.
func (s *Service) Settings(ctx context.Context, tx db.Tx[db.Tenant]) (*contracts.SiteSettings, error) {
	stored, err := s.stored(tx, false)
	if err != nil {
		return nil, err
	}
	if stored != nil {
		stored.PrimaryColor = stored.Resolve(s.palette)
		return stored, nil
	}
	// Not a row, and not an error either: every tenant has a site from the
	// moment it exists, and this is what it says before anybody has said
	// anything. Validate fills in the theme and resolves answers the colour.
	out := &contracts.SiteSettings{}
	if err := out.Validate(ctx); err != nil {
		return out, err
	}
	out.PrimaryColor = out.Resolve(s.palette)
	return out, nil
}

// SettingsForUpdate is Settings with the row locked FOR UPDATE.
//
// It is the read for a caller about to compare what it holds against what is
// stored — which is the only kind of comparison that needs a lock to mean
// anything: read unlocked, the row can move between the read and the write and
// the answer was about a revision that no longer exists. apps/platformkit hands
// this to modules/change as the subject's Lock; no other caller in this
// repository needs it, and nothing in this module branches on who does.
//
// A tenant with no row gets the defaults, whose Revision is 0: the number counts
// writes, and a row that does not exist has had none.
func (s *Service) SettingsForUpdate(ctx context.Context, tx db.Tx[db.Tenant]) (*contracts.SiteSettings, error) {
	stored, err := s.stored(tx, true)
	if err != nil {
		return nil, err
	}
	if stored != nil {
		stored.PrimaryColor = stored.Resolve(s.palette)
		return stored, nil
	}
	out := &contracts.SiteSettings{}
	if err := out.Validate(ctx); err != nil {
		return out, err
	}
	out.PrimaryColor = out.Resolve(s.palette)
	return out, nil
}

// Save writes the settings and says so, unless nothing changed. See
// contracts.Service.
func (s *Service) Save(ctx context.Context, tx db.Tx[db.Tenant], in *contracts.SiteSettings) (*contracts.SiteSettings, error) {
	// FOR UPDATE, and not the unlocked read Settings uses: this read is the
	// before-image of a diff the trail will keep forever. Read unlocked, two
	// concurrent saves both diff against the same row and the second one's trail
	// row says "from X to Y" when the value it replaced was the first save's Y —
	// history that did not happen, in the one table whose job is to have happened.
	stored, err := s.stored(tx, true)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		// The tenant's first save. crud.Create validates, stamps the tenant and
		// gives the row its id; the unique index is what keeps it a singleton
		// if two requests arrive at once.
		crud.Reset(in)
		in.Revision = 1
		in.ClearInstallationAccent(s.palette)
		if err := crud.Create(ctx, tx, in); err != nil {
			return nil, err
		}
		changes, err := events.Changes(nil, in, changed...)
		if err != nil {
			return nil, err
		}
		in.PrimaryColor = in.Resolve(s.palette)
		return in, publish(ctx, tx, in, changes)
	}
	// The id and the timestamps stay the stored row's: this is an update of the
	// one row there is, whatever the body claimed about identity.
	in.Base = stored.Base
	// Validated here as well as by crud.Update below, because the comparison
	// that decides whether anything changed has to run against the normalised
	// values: " Acme " and "Acme" are the same title, and a save that published
	// because of the whitespace would be a cache invalidated for nothing.
	//
	// The refusal is returned as the entity wrote it, with nothing added: a
	// fault.Reason carries its own verdict, so errors.Is still answers
	// ErrInvalid and kit/rest still answers 422, while the sentence a person is
	// shown stays the one sentence its writer keyed. Prefixing this package's
	// prose onto it would be a second sentence standing where a translated one
	// belongs.
	// because of the whitespace would be a cache invalidated for nothing. The
	// wrapping is kit/crud's, so both doors answer with the same 422. The
	// installation's own colour is cleared first, for the same reason: the value a
	// form echoes back from a read is not an edit, and the diff below must see the
	// value this save will store.
	in.ClearInstallationAccent(s.palette)
	if err := in.Validate(ctx); err != nil {
		return nil, err
	}
	changes, err := events.Changes(stored, in, changed...)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		// Nothing moved, so the row and its count stay where they are; the colour a
		// reader is shown is still the one this installation draws with.
		stored.PrimaryColor = stored.Resolve(s.palette)
		return stored, nil
	}
	// The write moves the count, and it moves it here rather than in a trigger or
	// a returning clause because the number is the row's own answer to "how many
	// times has this been saved" — a fact a diff is checked against.
	in.Revision = stored.Revision + 1
	if err := crud.Update(ctx, tx, in, columns...); err != nil {
		return nil, err
	}
	in.PrimaryColor = in.Resolve(s.palette)
	return in, publish(ctx, tx, in, changes)
}

// columns are the eight a save writes, and the stamp. They are written out
// rather than left to a whole-row update so that a column added later has to be
// added here too — the alternative is a field nobody can save and nobody
// notices.
var columns = []string{"title", "tagline", "home_slug", "theme", "primary_color", "logo_file_id", "nav", "revision", "updated_at"}

// stored is the tenant's row, or nil when it has none. lock takes the row
// FOR UPDATE, which is what SettingsForUpdate asked for and what an unlocked
// read must not do: the ordinary read runs inside the request's transaction too,
// and a read of the settings form locking the row would queue every apply behind
// every page view.
func (s *Service) stored(tx db.Tx[db.Tenant], lock bool) (*contracts.SiteSettings, error) {
	q := tx.DB()
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row contracts.SiteSettings
	err := q.Where("deleted_at IS NULL").Take(&row).Error
	switch classified := crud.Classify(err); {
	case classified == nil:
		// char(7) is blank-padded, and the value that means "this site named no
		// accent" is the empty one, so the column reads back as spaces. Trim it at the
		// one door every read of this row comes through, so the diff a save makes, the
		// resolution below and every caller see the value Validate wrote rather than
		// the column's padding. A colour is seven characters, which is why nothing
		// noticed the padding until the column could hold nothing.
		row.PrimaryColor = strings.TrimSpace(row.PrimaryColor)
		return &row, nil
	case errors.Is(classified, crud.ErrNotFound):
		return nil, nil
	default:
		return nil, classified
	}
}

// changed are the payload's own names for the seven values a save writes — the same
// seven columns above, in the spelling the event speaks. It is an allow-list, and it
// is the rule that replaced same(): a column that moves without being named here is a
// save the trail cannot explain, and one test over the two lists is what keeps them
// together.
var changed = []string{"title", "tagline", "homeSlug", "theme", "primaryColor", "logoFileId", "nav"}

func publish(ctx context.Context, tx db.Tx[db.Tenant], s *contracts.SiteSettings, changes []events.Change) error {
	return events.Publish(ctx, tx, contracts.EventSettingsUpdated, contracts.SettingsUpdated{
		SettingsID: s.ID, Title: s.Title, HomeSlug: s.HomeSlug, Theme: s.Theme,
		At: db.Now(), Changes: changes,
	})
}
