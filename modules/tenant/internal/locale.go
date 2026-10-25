package internal

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// SetLocale says which languages one tenant is served in, and which of them is the
// one to fall back to. Both halves are written in the caller's transaction and one
// event says so, because a tenant whose default is not in its own set is a request
// answered in a language that tenant does not serve.
func (s *Service) SetLocale(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in contracts.SetLocale) (*contracts.Tenant, error) {
	wanted, err := s.validLocales(in)
	if err != nil {
		return nil, err
	}
	t, err := s.lock(tx, id)
	if err != nil {
		return nil, err
	}
	if t.DefaultLocale == wanted[0] && slices.Equal(t.Locales, wanted[1:]) {
		return t, nil
	}
	t.DefaultLocale, t.UpdatedAt = wanted[0], db.Now()
	if err := tx.DB().Model(t).Select("default_locale", "updated_at").Updates(t).Error; err != nil {
		return nil, crud.Classify(err)
	}
	if err := tx.DB().Exec("DELETE FROM tenant_locales WHERE tenant_id = ?", t.ID).Error; err != nil {
		return nil, crud.Classify(err)
	}
	if err := s.serve(tx, t.ID, wanted...); err != nil {
		return nil, err
	}
	// The same argument as a suspension's: the host resolution believes a tenant
	// for half a minute, and the languages of a page are not the languages of the
	// page a person is looking at once the set behind it changed.
	if err := events.SucceedFor(ctx, tx, db.Now()); err != nil {
		return nil, err
	}
	err = events.PublishFor(ctx, tx, t.ID, contracts.EventLocaleSet, contracts.LocaleSet{
		TenantID: t.ID, Default: wanted[0], Supported: wanted[1:], At: t.UpdatedAt,
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tx, id)
}

// validLocales is the command's rule, in one place: every tag is a tag, every tag is
// one this installation has copy for, the default is the first thing returned, and
// the set behind it is sorted, deduplicated and without the default — which is the
// shape contracts.Tenant's two fields hold them in, so the comparison above that
// decides "nothing changed" is one comparison and not a set operation in three
// places.
//
// The second condition is the one that stops a page lying about its language: a
// tenant served in a language no catalogue carries is a page that declares that
// language and shows the source copy, which is what a browser, a screen reader and
// a translation tool are then told. The installation knows which languages it can
// answer in — it is the composition that read the files — so the composition says,
// and this command refuses anything else rather than finding out at render time.
func (s *Service) validLocales(in contracts.SetLocale) ([]string, error) {
	spoken := s.spoken()
	defaultTag, err := contracts.ValidLocale(in.Default)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	if spoken != nil && !spoken[defaultTag] {
		return nil, fmt.Errorf("%w: %s is not a language this installation has copy for", crud.ErrInvalid, defaultTag)
	}
	set := make([]string, 0, len(in.Supported))
	for _, tag := range in.Supported {
		canonical, err := contracts.ValidLocale(tag)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
		}
		if spoken != nil && !spoken[canonical] {
			return nil, fmt.Errorf("%w: %s is not a language this installation has copy for", crud.ErrInvalid, canonical)
		}
		if canonical != defaultTag && !slices.Contains(set, canonical) {
			set = append(set, canonical)
		}
	}
	slices.Sort(set)
	return append([]string{defaultTag}, set...), nil
}

// localesOf is the set of languages a tenant is served in, minus the one it is
// served in by default: the table holds every language including that one, so the
// row is never a list whose first element happens to be the default, and the entity
// keeps the pair the way its two fields hold it.
func (s *Service) localesOf(tx db.Tx[db.System], id uuid.UUID, except string) ([]string, error) {
	var stored []string
	err := tx.DB().Table("tenant_locales").Where("tenant_id = ? AND locale <> ?", id, except).
		Order("locale").Pluck("locale", &stored).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	return stored, nil
}

// spoken is the set of languages this installation can answer in, canonicalised, or
// nil when the composition declared none and there is therefore nothing to check a
// tenant's choice against. An entry that is not a tag is left out rather than
// trusted: it names a language no catalogue file could be read for, and a tenant
// asking for it is refused by the same rule.
func (s *Service) spoken() map[string]bool {
	if len(s.langs) == 0 {
		return nil
	}
	out := make(map[string]bool, len(s.langs))
	for _, tag := range s.langs {
		if canonical, err := contracts.ValidLocale(tag); err == nil {
			out[canonical] = true
		}
	}
	return out
}

// readDefaultLocale takes the language the database just chose for a tenant it
// created. The column's default is the deployment's decision and this module never
// writes it on a create, so the value has to come back rather than be assumed.
func (s *Service) readDefaultLocale(tx db.Tx[db.System], t *contracts.Tenant) error {
	var tag string
	err := tx.DB().Table("tenants").Where("id = ?", t.ID).Pluck("default_locale", &tag).Error
	if err != nil {
		return crud.Classify(err)
	}
	t.DefaultLocale = tag
	return nil
}
