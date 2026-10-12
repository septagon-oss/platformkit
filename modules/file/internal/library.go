package internal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// The library's three reads and one write. Everything about them is decided in
// contracts — the family a row belongs to, the six orders, what a caption may
// claim — so that the SQL below and filetest.Fake answer the same question
// rather than each keeping a copy of the answer.

// essenceSQL is the media type's essence as SQL: the type without its
// parameters, lower-cased and trimmed, which is the string contracts.Essence
// reads a Go media type into. The two are the same expression, and the family
// filter only means anything while they are.
const essenceSQL = `btrim(split_part(lower(content_type), ';', 1))`

// Search is the grid. See contracts.Library.
func (s *Service) Search(ctx context.Context, tx db.Tx[db.Tenant], q contracts.LibraryQuery) ([]*contracts.File, int64, error) {
	q, err := q.Normalize()
	if err != nil {
		return nil, 0, err
	}
	field, desc, err := contracts.LibrarySort(q.Sort)
	if err != nil {
		return nil, 0, err
	}
	stmt := tx.DB().WithContext(ctx).Model(&contracts.File{})
	// Soft-deleted rows are gone from the library whether or not the bytes have
	// been removed yet: a tile for a file whose download 404s is a lie about a
	// picture somebody has.
	stmt = stmt.Where("deleted_at IS NULL")
	if q.Name != "" {
		// A bound parameter, so what a person types is matched case-insensitively
		// against the name and nothing else: a % or _ in the box stays a wildcard,
		// because the pattern is a pattern and the box makes no promise about one.
		stmt = stmt.Where(`name ILIKE ?`, "%"+q.Name+"%")
	}
	switch q.Family {
	case contracts.FamilyImage:
		stmt = stmt.Where(essenceSQL+` IN ? AND width > 0`, contracts.ImageTypes)
	case contracts.FamilyVideo:
		stmt = stmt.Where(essenceSQL+` IN ?`, contracts.VideoTypes)
	case contracts.FamilyDocument:
		stmt = stmt.Where(essenceSQL+` NOT IN ? AND NOT (`+essenceSQL+` IN ? AND width > 0)`,
			contracts.VideoTypes, contracts.ImageTypes)
	}
	if q.Unused {
		stmt = stmt.Where("unused_since IS NOT NULL")
	}
	var total int64
	if err := stmt.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("file: count this tenant's library: %w", err)
	}
	var rows []*contracts.File
	err = stmt.Order(orderColumn(field, desc)).Limit(q.Limit).Offset(q.Offset).Find(&rows).Error
	if err != nil {
		return nil, 0, fmt.Errorf("file: list this tenant's library: %w", err)
	}
	return rows, total, nil
}

// orderColumn is the column one of the six sort tokens orders by. The token has
// already been checked against contracts.Sorts, so this maps a field the allow
// list named and never reads a caller's string.
func orderColumn(field string, desc bool) string {
	column := "created_at"
	switch field {
	case "Name":
		column = "name"
	case "Size":
		column = "size"
	}
	if desc {
		return column + " DESC, id DESC"
	}
	return column + " ASC, id DESC"
}

// Quota is the meter. See contracts.Library.
//
// It takes no advisory lock, on purpose: the lock exists so that twenty uploads
// cannot each read the same total and each decide there is room, and a page load
// that took it would queue behind the tenant's own uploads — an outage with a
// friendly face. The number is therefore the sum as of this transaction's own
// snapshot and can be one commit stale; the refusal that protects the ceiling is
// charge's, and it recomputes the same sum under the lock before it inserts.
func (s *Service) Quota(ctx context.Context, tx db.Tx[db.Tenant]) (*contracts.Quota, error) {
	used, widths, err := usage(ctx, tx)
	if err != nil {
		return nil, err
	}
	return &contracts.Quota{
		Used: used, Limit: s.quota, Unlimited: s.quota <= 0, Variants: widths,
	}, nil
}

// SetAlt writes what a picture says about itself. See contracts.Library.
func (s *Service) SetAlt(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, alt string, decorative bool, expected time.Time) (*contracts.File, error) {
	f, err := crud.GetForUpdate[*contracts.File](tx, id)
	if err != nil {
		return nil, err
	}
	// A caller that names no instant asked for the write whatever anybody else did,
	// and this module has no such command.
	if expected.IsZero() {
		return nil, fmt.Errorf("%w: %s: an alt text is written against the file you were shown", crud.ErrInvalid, id)
	}
	// The row is locked above, before its own clock is read, which is the only
	// order that makes the comparison mean anything: two captions written
	// together settle one at a time, and the one that arrived second is refused
	// rather than silently winning.
	if !f.UpdatedAt.Equal(expected) {
		return nil, fmt.Errorf("%w: this file changed while you were looking — it is at %s, and you were shown %s",
			crud.ErrConflict, f.UpdatedAt.UTC().Format(time.RFC3339Nano), expected.UTC().Format(time.RFC3339Nano))
	}
	// One refusal for both commands and every door, held by contracts: a caption
	// that is empty without being declared decorative, or that is both.
	if err := contracts.AltClaim(alt, decorative); err != nil {
		return nil, err
	}
	f.Alt, f.Decorative = strings.TrimSpace(alt), decorative
	// The clock is written here rather than left to the driver's own tracking,
	// because it is the number the next writer will be refused against: the row
	// the caller was shown and the row that carries the new caption have to
	// disagree by exactly this write, on both implementations and not by luck.
	f.UpdatedAt = db.Now()
	if err := crud.Update(ctx, tx, f, "alt", "decorative", "updated_at"); err != nil {
		return nil, err
	}
	return f, events.Publish(ctx, tx, contracts.EventAltered, contracts.Altered{
		FileID: f.ID, Alt: f.Alt, Decorative: f.Decorative,
		Size: f.Size, Width: f.Width, Height: f.Height, At: db.Now(),
	})
}

var _ contracts.Library = (*Service)(nil)
