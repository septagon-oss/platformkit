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
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
)

// Deps is what the lifecycle cannot make for itself: the one collaborator a
// publication needs beyond this module's own rows.
type Deps struct {
	// Shown is the file module's audience door, called inside the publish.
	Shown rest.FileShown
}

// Service is the content lifecycle. Everything a command needs about a row
// arrives with the transaction it is given, which is what lets one instance serve
// a request and an event handler at once. The one thing no row carries is who is
// allowed to read the images its body shows, and that arrives as Shown.
type Service struct {
	deps Deps
}

// NewService returns the lifecycle commands. module.go constructs it. An
// unwired Shown becomes rest.ShowNothing, which is the answer the empty field
// gave before this collaborator existed: publish everything, flip no audience.
func NewService(deps Deps) *Service {
	if deps.Shown == nil {
		deps.Shown = rest.ShowNothing{}
	}
	return &Service{deps: deps}
}

var _ contracts.Service = (*Service)(nil)

// Publish serves it to anybody, and records when. See contracts.Service.
func (s *Service) Publish(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	c, err := crud.GetForUpdate[*contracts.Content](tx, id)
	if err != nil {
		return nil, err
	}
	// The duty is asked before the row's status is read, because it is about who
	// is asking: a refusal this command has no row to return, no state to move and
	// nothing to say about it afterwards.
	actor, _ := tenancy.ActorFrom(ctx)
	if err := c.PublishBy(actor); err != nil {
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
	// The images go public before the page does, in this same transaction: a
	// publication that committed its status and then flipped the audience would
	// leave a window in which the public page names bytes its own ledger says are
	// private, which is a broken image and not a delay. A body that names a file
	// this record files no use of refuses the publish rather than exposing it.
	if err := s.show(ctx, tx, c, true); err != nil {
		return nil, err
	}
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
	c, err := crud.GetForUpdate[*contracts.Content](tx, id)
	if err != nil {
		return nil, err
	}
	if c.Status == status {
		return c, nil
	}
	c.Status, c.PublishedAt = status, nil
	// Nothing is showing this record to anybody anymore, so no image of it is
	// either. Naming no files says "every file this record shows", and this arm
	// never refuses: taking bytes away from the public is the safe direction.
	if err := s.show(ctx, tx, c, false); err != nil {
		return nil, err
	}
	return c, s.move(ctx, tx, c, event, db.Now())
}

// show asserts this record's audience for the images its body names. The ids come
// from the body this transaction is writing, through the same parse the renderer
// and the use ledger use, so the set the audience is asserted for is the set that
// was filed as uses and not a second reading of the text somebody could disagree
// with. A body that will not parse has nothing to assert: the write that accepted
// it validated it, and a publish that could not re-parse it asserts nothing rather
// than publishing half of it public.
func (s *Service) show(ctx context.Context, tx db.Tx[db.Tenant], c *contracts.Content, public bool) error {
	var refs []uuid.UUID
	if doc, err := richtext.Parse(c.Body); err == nil {
		for _, ref := range richtext.References(doc) {
			if ref.ID != uuid.Nil {
				refs = append(refs, ref.ID)
			}
		}
	}
	return s.deps.Shown.SetShown(ctx, tx, rest.ShownInput{
		Module: "content", Entity: "content", Record: c.ID, Files: refs, Public: public,
	})
}

// move writes the two columns the lifecycle owns and says what happened.
func (s *Service) move(ctx context.Context, tx db.Tx[db.Tenant], c *contracts.Content, event string, at time.Time) error {
	// A lifecycle move is a write of the row, so the row's own write count moves
	// with it: a page published between the moment a person read its history and
	// the moment they asked for a restore is a page the restore refuses rather
	// than one it overwrites. The version series skips this number — a version is
	// a write's content, and these writes move status, not content — which is why
	// a series is keyed by the number rather than running unbroken.
	c.Revision++
	if err := crud.Update(ctx, tx, c, "status", "published_at", "updated_at", "revision"); err != nil {
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
