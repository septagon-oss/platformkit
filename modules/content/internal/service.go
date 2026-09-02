// Package internal is every implementation of the content module. Nothing
// outside modules/content can import it, which is the compiler enforcing idea
// 3: a consumer takes contracts.Service, and taking anything else does not
// build.
package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
)

// Service is the content lifecycle. It has no fields: everything a command
// needs arrives with the transaction it is given, which is what lets one
// instance serve a request and an event handler at once.
type Service struct{}

// NewService returns the lifecycle commands. module.go constructs it.
func NewService() *Service { return &Service{} }

var _ contracts.Service = (*Service)(nil)

// Publish serves it to anybody, and records when. See contracts.Service.
func (s *Service) Publish(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	c, err := crud.Get[*contracts.Content](tx, id)
	if err != nil {
		return nil, err
	}
	switch c.Status {
	case contracts.StatusPublished:
		// The publication time does not move: a page was published once, and a
		// second click on the button is not a second publication.
		return c, nil
	case contracts.StatusArchived:
		return nil, fmt.Errorf("%w: archived content is not published from the archive; unpublish it first", crud.ErrConflict)
	}
	at := db.Now()
	c.Status, c.PublishedAt = contracts.StatusPublished, &at
	return c, s.move(ctx, tx, c, contracts.EventPublished, at)
}

// Unpublish takes it back to a draft. See contracts.Service.
func (s *Service) Unpublish(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	return s.to(ctx, tx, id, contracts.StatusDraft, contracts.EventUnpublished)
}

// Archive keeps it and serves it to nobody. See contracts.Service.
func (s *Service) Archive(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	return s.to(ctx, tx, id, contracts.StatusArchived, contracts.EventArchived)
}

// to moves content to a status that is not published, from any other. Both
// commands that use it clear the publication time, because "published means
// published at a time" is one fact and this is the half that puts it away.
func (s *Service) to(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, status, event string) (*contracts.Content, error) {
	c, err := crud.Get[*contracts.Content](tx, id)
	if err != nil {
		return nil, err
	}
	if c.Status == status {
		return c, nil
	}
	c.Status, c.PublishedAt = status, nil
	return c, s.move(ctx, tx, c, event, db.Now())
}

// move writes the two columns the lifecycle owns and says what happened.
func (s *Service) move(ctx context.Context, tx db.Tx[db.Tenant], c *contracts.Content, event string, at time.Time) error {
	if err := crud.Update(ctx, tx, c, "status", "published_at", "updated_at"); err != nil {
		return err
	}
	return events.Publish(ctx, tx, event, contracts.Moved{
		ContentID: c.ID, Slug: c.Slug, Kind: c.Kind, Status: c.Status, At: at,
	})
}

// Public is the published content at this slug. The status is in the query
// rather than checked afterwards, so a draft and a slug nobody has used are one
// answer and the caller cannot tell which it was.
//
// The slug is normalised the same way a write normalises it, because a name
// stored one way and looked up another is a page nobody can reach.
func (s *Service) Public(_ context.Context, tx db.Tx[db.Tenant], slug string) (*contracts.Content, error) {
	var c contracts.Content
	err := tx.DB().Where("slug = ? AND status = ? AND deleted_at IS NULL",
		contracts.Slugify(slug), contracts.StatusPublished).Take(&c).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	return &c, nil
}
