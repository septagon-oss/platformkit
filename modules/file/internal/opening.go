package internal

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// Open is the row and its bytes. See contracts.Service.
func (s *Service) Open(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, anonymous bool) (*contracts.File, io.ReadCloser, error) {
	f, err := crud.Get[*contracts.File](tx, id)
	if err != nil {
		return nil, nil, err
	}
	if anonymous {
		may, err := shownRead(ctx, tx, f)
		if err != nil {
			return nil, nil, err
		}
		if !may {
			// Not a 403: a caller who is not signed in learns nothing about what
			// this tenant has, including whether it has this.
			return nil, nil, crud.ErrNotFound
		}
	}
	return s.openKey(ctx, tx, f, contracts.Key(f.StorageKey))
}

// openKey is the one read of the store every door ends at: the row already
// checked, and the bytes at one key.
func (s *Service) openKey(ctx context.Context, tx db.Tx[db.Tenant], f *contracts.File, key contracts.Key) (*contracts.File, io.ReadCloser, error) {
	body, err := s.storage.Get(ctx, contracts.ScopeOfTx(tx), key)
	if err != nil {
		// A row that says there are bytes and a store that has none is the one
		// inconsistency the split can produce, and it is an outage rather than
		// something the caller did: it reaches huma as a 500 with this in the log.
		return nil, nil, fmt.Errorf("file: open %s for %s: %w", key, f.ID, err)
	}
	return f, body, nil
}

// OpenWidth is Open at one planned width. See contracts.WidthOpener.
//
// There is no fallback to the original, and that is the whole design of the
// door: one address with one width names one object, or every cache between this
// process and a reader becomes a lie about which of the two it is holding. A
// width with no ready variant is therefore the same 404 a private file gets at
// the public door, so an anonymous caller learns nothing about which widths this
// tenant has waiting to be built.
func (s *Service) OpenWidth(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, anonymous bool, width int) (*contracts.File, io.ReadCloser, error) {
	if width <= 0 {
		return s.Open(ctx, tx, id, anonymous)
	}
	f, err := crud.Get[*contracts.File](tx, id)
	if err != nil {
		return nil, nil, err
	}
	if anonymous {
		may, err := shownRead(ctx, tx, f)
		if err != nil {
			return nil, nil, err
		}
		if !may {
			return nil, nil, crud.ErrNotFound
		}
	}
	if width == f.Width {
		// The frame's own width asked for by number: the same object the bare
		// address serves, because a width is never planned equal to the frame's
		// own and this address has exactly one thing to answer with.
		return s.openKey(ctx, tx, f, contracts.Key(f.StorageKey))
	}
	var row contracts.Variant
	err = tx.DB().WithContext(ctx).Where("file_id = ? AND width = ? AND state = ? AND deleted_at IS NULL",
		id, width, contracts.VariantReady).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, fmt.Errorf("%w: no %d pixel width of %s is built", crud.ErrNotFound, width, id)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("file: which widths %s serves: %w", id, err)
	}
	return s.openKey(ctx, tx, atWidth(f, &row), contracts.Key(row.StorageKey))
}

// atWidth is the row a reader of one width's bytes is told about: the parent's
// name, type and visibility — a width is the same picture, and the container was
// decided once, on the frame — and the numbers of the object standing at this
// address.
//
// `size` is what makes this a cure rather than a tidying: contracts.ContentResponse
// answers every body that cannot seek with Content-Length read from the row it was
// handed, so a width served with its parent's size is a response whose own header
// contradicts the bytes it sends — true of any Storage whose Get streams (the two
// this module ships return a seekable file and an object, so only an adapter written
// outside this repository meets it today, and the port does not promise seekability).
// sha256, width, height and updated_at travel for the same reason and no more: they
// are what a caller checks or caches about the object it is being sent, and a row
// that describes the frame beside a body that is a width describes something else.
//
// The copy is not cosmetic. crud.Get handed back the row this transaction read, and
// nothing on a read path has any business rewriting it.
func atWidth(f *contracts.File, row *contracts.Variant) *contracts.File {
	out := *f
	out.Size, out.SHA256 = row.Size, row.SHA256
	out.Width, out.Height = row.Width, row.Height
	out.UpdatedAt = row.UpdatedAt
	return &out
}

// Variants is the widths one file can serve today, smallest first: the read a
// srcset is built from, and the same answer the door serves a ?w= off, so the
// two cannot disagree about which widths exist.
//
// A ready row is the whole question. A pending one is not servable and a failed
// one never will be, and a caller that wants to know what is *waiting* reads the
// rows themselves in the media library rather than through a port that lays
// images out.
func (s *Service) Variants(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID) ([]contracts.Variant, error) {
	if _, err := crud.Get[*contracts.File](tx, fileID); err != nil {
		return nil, err
	}
	var rows []contracts.Variant
	if err := tx.DB().WithContext(ctx).Where("file_id = ? AND state = ? AND deleted_at IS NULL",
		fileID, contracts.VariantReady).Order("width").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("file: which widths %s serves: %w", fileID, err)
	}
	return rows, nil
}

// union is the ascending, deduplicated id list every lock is taken in.
func union(a, b []uuid.UUID) []uuid.UUID {
	return slices.Compact(ascending(append(slices.Clone(a), b...)))
}

// ascending puts file ids in the one order every lock pass here takes them, so no
// two commands can arrive at two files they share from opposite ends. A caller
// names its files in the order its body holds them, which is nobody's lock order.
func ascending(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(x, y uuid.UUID) int { return bytes.Compare(x[:], y[:]) })
	return out
}

// lockFileForUpdate is lockFileKeyShare's stronger sibling, for the two commands
// that decide a file's audience — SetShown, and the rewrite that ends a use — each of
// which answers who may read the file off a read of every live use of it, and the
// lock is what makes that read repeatable against another publish of the same image.
// The rewrite that ends a use needs it for one thing more: the count of uses left is
// the grace period's start, and only an exclusive lock makes it the last word.
func lockFileForUpdate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (bool, error) {
	var found uuid.UUID
	err := tx.DB().WithContext(ctx).Raw(`SELECT id FROM files WHERE id = ? FOR UPDATE`, id).Row().Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("file: lock %s to say who may read it: %w", id, err)
	}
	return true, nil
}

// missing is the first id in want that have does not name, for the refusal that
// has to say which file the caller means.
func missing(want, have []uuid.UUID) uuid.UUID {
	for _, id := range want {
		if !slices.Contains(have, id) {
			return id
		}
	}
	return uuid.Nil
}

// lockFileKeyShare takes FOR KEY SHARE on one file's row and says whether the
// row was there for this tenant to find. It refuses for real errors only: what
// a missing row costs is the caller's decision, and SetUses decides it two ways.
//
// There is no second check of whose the row is, because there is nothing to
// check it against: the policy decides what this transaction can see, and a row
// it cannot see answers no rows. That is what makes a use of another tenant's
// file impossible rather than merely not allowed.
func lockFileKeyShare(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (bool, error) {
	var found uuid.UUID
	err := tx.DB().WithContext(ctx).Raw(`SELECT id FROM files WHERE id = ? FOR KEY SHARE`, id).Row().Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("file: lock %s to record a use of it: %w", id, err)
	}
	return true, nil
}
