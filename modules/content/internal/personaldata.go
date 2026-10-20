// This file is this module's answer to one contract: personaldata.Data (ADR 0020).
//
// Export is a read of the rows this module owns about one person, in the shape this
// module's own contracts use. Erase removes what those rows *say* and keeps the rows
// wherever the audit trail has to keep a row; the person's id survives, the strings
// that named them do not. Both run inside the caller's transaction, inside the tenant
// row-level security already bounded, so neither names tenant_id.
//
// It is a contributor and never a command: no code in this module calls it. The module
// that composes the parts (modules/subject) does, and the kernel reads the same
// declaration below off the manifest to refuse a column this file does not scrub.
package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
)

// personalData is what a person wrote for the tenant's own site. The page stays — a
// published page that silently disappeared is a lie about the tenant's site and about
// its reader's bookmarks — and the author_id stays with it, because "who wrote this" is
// answerable as an id long after "who wrote this" is answerable as a name. The title and
// the body are scrubbed, which is where the person's own words about themselves sit.
type personalData struct{}

// PersonalData is this module's contribution to a person's export and erasure.
func PersonalData() personaldata.Part {
	return personaldata.Part{Module: "content", Data: personalData{}}
}

type contentPart struct {
	Contents []contentRow `json:"contents"`
}

type contentRow struct {
	ID          uuid.UUID  `json:"id"`
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Kind        string     `json:"kind"`
	Status      string     `json:"status"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant],
	s personaldata.Subject) (json.RawMessage, error) {
	var rows []contentRow
	err := tx.DB().WithContext(ctx).Table("contents").
		Select("id, slug, title, body, kind, status, published_at, created_at").
		Where("author_id = ?", s.ID).Order("created_at, id").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("content: export %s's pages: %w", s.ID, err)
	}
	return json.Marshal(contentPart{Contents: rows})
}

// Erase scrubs every page in the tenant, not only the ones this person authored: a
// colleague's post that quotes them by name is their personal data too, and the page
// keeps its author either way.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	_, err := personaldata.Scrub(ctx, tx, s, "contents", []string{"title", "body"}, "")
	return err
}
