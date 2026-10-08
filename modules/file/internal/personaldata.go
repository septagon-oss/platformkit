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

// personalData is the byte store's metadata. Two rules make it narrower than it looks:
//
//   - What export answers is metadata, and the manifest says what it did not attach.
//     The bytes of a tenant's object store do not belong in one zip (ADR 0020 §5), so
//     the name, content type, size, digest and dates travel, and a bundle assembled
//     over MaxBundleBytes names every file it left out rather than being silently short.
//   - Erase does not delete the person's own bytes. Service.EraseSubject answers a
//     retention question with a certificate that bytes are gone; Article 17 asks for a
//     copy and for the identifying record. The two commands stay distinct and a request
//     says which one ran.
//
// file_uses is the one set erase deletes: a use row says which field of which record
// displayed which file, and migration 000045 rewrites that set hard, so a row naming a
// record that no longer exists is a use that ended. Its `record` is a polymorphic id —
// module and entity say whose row it is — so it names this person's own record whenever
// the richtext field that displays the file is theirs.
type personalData struct{}

// PersonalData is this module's contribution to a person's export and erasure.
func PersonalData() personaldata.Part { return personaldata.Part{Module: "file", Data: personalData{}} }

type filePart struct {
	Files    []fileRow    `json:"files"`
	Holds    []holdRow    `json:"holds"`
	Erasures []erasureRow `json:"erasures"`
	Uses     []useRow     `json:"uses"`
	LeftOut  []leftOutRow `json:"leftOut"`
}

type fileRow struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	Visibility  string    `json:"visibility"`
	CreatedAt   time.Time `json:"createdAt"`
}

type holdRow struct {
	ID       uuid.UUID  `json:"id"`
	FileID   string     `json:"fileId"`
	Reason   string     `json:"reason"`
	Until    *time.Time `json:"until,omitempty"`
	PlacedBy string     `json:"placedBy,omitempty"`
}

type erasureRow struct {
	ID      uuid.UUID `json:"id"`
	FileID  string    `json:"fileId"`
	SHA256  string    `json:"sha256"`
	Cause   string    `json:"cause"`
	Reason  string    `json:"reason"`
	ActedAt time.Time `json:"actedAt"`
}

type useRow struct {
	FileID string `json:"fileId"`
	Module string `json:"module"`
	Entity string `json:"entity"`
	Field  string `json:"field"`
	Locale string `json:"locale"`
}

type leftOutRow struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Size   int64     `json:"size"`
	SHA256 string    `json:"sha256"`
	Why    string    `json:"why"`
}

func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant],
	s personaldata.Subject) (json.RawMessage, error) {
	var out filePart
	db_ := tx.DB().WithContext(ctx)
	err := db_.Table("files").
		Select("id, name, content_type, size, sha256, visibility, created_at").
		Where("uploader_id = ?", s.ID).Order("created_at, id").Find(&out.Files).Error
	if err != nil {
		return nil, fmt.Errorf("file: export %s's files: %w", s.ID, err)
	}
	err = db_.Table("file_holds").
		Select("id, file_id::text AS file_id, reason, until, placed_by::text AS placed_by").
		Where("placed_by = ? OR file_id IN (SELECT id FROM files WHERE uploader_id = ?)", s.ID, s.ID).
		Order("created_at, id").Find(&out.Holds).Error
	if err != nil {
		return nil, fmt.Errorf("file: export %s's holds: %w", s.ID, err)
	}
	err = db_.Table("file_erasures").
		Select("id, file_id::text AS file_id, sha256, cause, reason, created_at AS acted_at").
		Where("subject_id = ? OR actor = ?", s.ID, s.ID).Order("created_at, id").Find(&out.Erasures).Error
	if err != nil {
		return nil, fmt.Errorf("file: export %s's erasure certificates: %w", s.ID, err)
	}
	err = db_.Table("file_uses").
		Select("file_id::text AS file_id, module, entity, field, locale").
		Where("record = ?", s.ID).Order("created_at, id").Find(&out.Uses).Error
	if err != nil {
		return nil, fmt.Errorf("file: export %s's uses: %w", s.ID, err)
	}
	return json.Marshal(out)
}

// Erase keeps every id and removes the sentences: a hold's reason and a certificate's
// reason are what somebody wrote, and a file's name is often the person's own word for
// it ("passport-scan.jpg"). Names and reasons are scrubbed across the tenant for the
// same reason the other modules are: the row that *mentions* this person need not carry
// their id anywhere.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	for _, t := range []struct {
		table   string
		columns []string
	}{
		{"files", []string{"name"}},
		{"file_holds", []string{"reason"}},
		{"file_erasures", []string{"reason"}},
	} {
		if _, err := personaldata.Scrub(ctx, tx, s, t.table, t.columns, ""); err != nil {
			return err
		}
	}
	return tx.DB().WithContext(ctx).Exec("DELETE FROM file_uses WHERE record = ?", s.ID).Error
}
