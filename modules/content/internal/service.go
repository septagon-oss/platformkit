// Package internal is every implementation of the content module. Nothing
// outside modules/content can import it, which is the compiler enforcing idea
// 3: a consumer takes contracts.Service, and taking anything else does not
// build.
package internal

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
)

// Service is the content lifecycle. It has no fields: everything a command
// needs arrives with the transaction it is given, which is what lets one
// instance serve a request and an event handler at once. The rules are not
// here either: each command is contracts' decision, applied to a row by
// crud.Apply at the transaction's instant.
type Service struct{}

// NewService returns the lifecycle commands. module.go constructs it.
func NewService() *Service { return &Service{} }

var _ contracts.Service = (*Service)(nil)

// lifecycle is the three columns the decisions own.
var lifecycle = []string{"status", "published_at", "updated_at"}

// Publish serves it to anybody, and records when. See contracts.Publish.
func (s *Service) Publish(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	return crud.Apply(ctx, tx, id, func(c contracts.Content) (crud.Outcome[contracts.Content], error) {
		return contracts.Publish(c, tx.At())
	}, lifecycle...)
}

// Unpublish takes it back to a draft. See contracts.Unpublish.
func (s *Service) Unpublish(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	return crud.Apply(ctx, tx, id, func(c contracts.Content) (crud.Outcome[contracts.Content], error) {
		return contracts.Unpublish(c, tx.At()), nil
	}, lifecycle...)
}

// Archive keeps it and serves it to nobody. See contracts.Archive.
func (s *Service) Archive(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	return crud.Apply(ctx, tx, id, func(c contracts.Content) (crud.Outcome[contracts.Content], error) {
		return contracts.Archive(c, tx.At()), nil
	}, lifecycle...)
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
