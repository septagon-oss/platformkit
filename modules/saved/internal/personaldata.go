package internal

import (
	"context"
	"encoding/json"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
)

type personalData struct{}

// PersonalData contributes one person's own retained copies to an authorized
// caller's export or erasure, inside that caller's tenant transaction.
func PersonalData() personaldata.Part {
	return personaldata.Part{Module: "saved", Data: personalData{}}
}

// personalRecord is the person's own bundle section: the copies as this module
// reads them, availability included is not — an export states what is held, not
// what a source would answer today.
type personalRecord struct {
	Items []contracts.Item `json:"items"`
}

// Export answers with every copy this person holds, newest first, which is the
// order the library shows them in.
func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) (json.RawMessage, error) {
	var rows []row
	if err := tx.DB().WithContext(ctx).Where("person_id = ?", s.ID).Order("captured_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := personalRecord{Items: make([]contracts.Item, 0, len(rows))}
	for i := range rows {
		out.Items = append(out.Items, rows[i].item())
	}
	return json.Marshal(out)
}

// Erase takes every copy away outright rather than scrubbing the words on it. A
// retained copy is one person's hold on somebody else's content: what it says is
// the source's title and values, so a scrub would leave behind a row naming a
// person, a source key and a captured document, which is exactly what the request
// asked to stop holding. The delete is unscoped for the reason Unsave removes
// rather than hides — a soft-deleted copy stays addressable by its own id. The
// release of a retained file's use arrives with the file ledger this composition
// does not yet wire (README §8): a copy written today names a file and records no
// use for it, so there is no use here to release.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	return tx.DB().WithContext(ctx).Unscoped().Where("person_id = ?", s.ID).Delete(&row{}).Error
}
