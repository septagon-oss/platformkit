// This file is this module's answer to one contract: personaldata.Data (ADR 0020).
//
// Export is a read of the rows this module owns about one person, in the shape this
// module's own contracts use. Erase removes what those rows *say*: a translation is a
// field of somebody else's record, so the row, its record_id and its translator stay,
// and the text an identifier was copied into does not. Both run inside the caller's
// transaction, inside the tenant row-level security already bounded, so neither names
// tenant_id.
//
// It is a contributor and never a command: no code in this module calls it. The module
// that composes the parts (modules/subject) does, and the kernel reads the same
// declaration off the manifest to refuse a column this file does not scrub.
package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
)

// personalData is the translation table. A person's own translations are the rows
// they wrote — the text is theirs, and the record it translates is not — so Export
// answers for `translator_id` and Erase scrubs the two text columns an identifier
// reaches: `value` and the `source_text` it was made from, which is a copy of another
// module's field and holds whatever that field held, an address included.
type personalData struct{}

// PersonalData is this module's contribution to a person's export and erasure.
func PersonalData() personaldata.Part {
	return personaldata.Part{Module: "translation", Data: personalData{}}
}

type translationPart struct {
	Translations []translationRow `json:"translations"`
}

type translationRow struct {
	Module   string `json:"module"`
	Entity   string `json:"entity"`
	RecordID string `json:"recordId"`
	Field    string `json:"field"`
	Locale   string `json:"locale"`
	Value    string `json:"value"`
	// SourceText is the copy this translation was made from, as its author saw it:
	// the row keeps it precisely so a person can be handed the text they worked on.
	SourceText string     `json:"sourceText"`
	Status     string     `json:"status"`
	Origin     string     `json:"origin"`
	CreatedAt  time.Time  `json:"createdAt"`
	ReviewedAt *time.Time `json:"reviewedAt,omitempty"`
}

func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant],
	s personaldata.Subject) (json.RawMessage, error) {
	var rows []translationRow
	// record_id cast to text: the bundle is a document a person reads, and the row
	// this id points at belongs to another module and may be gone.
	err := tx.DB().WithContext(ctx).Table("translations").
		Select("module, entity, record_id::text AS record_id, field, locale, value, "+
			"source_text, status, origin, created_at, reviewed_at").
		Where("translator_id = ?", s.ID).Order("created_at, id").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("translation: export %s's translations: %w", s.ID, err)
	}
	return json.Marshal(translationPart{Translations: rows})
}

// Erase scrubs value and source_text across the tenant's rows: the text a translated
// page carried may name this person without the row carrying their id anywhere, and it
// is the text they asked for, not the row. What it leaves is the row's own account of
// itself — status, origin, locale, and the source_hash, which keeps its length and
// stops digesting the scrubbed copy. That is the honest remainder: recomputing the
// digest would be a claim to know what the source said before it was scrubbed, which is
// exactly what an erasure is asked to forget.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	_, err := personaldata.Scrub(ctx, tx, s, "translations",
		[]string{"value", "source_text"}, "")
	return err
}
