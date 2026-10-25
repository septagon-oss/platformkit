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

// Erase answers for the subject twice, because one retained copy is two people's thing at
// once. The copies the subject kept are theirs and go outright — a scrub would leave a row
// naming a person, a source key and a captured document, which is what they asked to stop
// holding — and unscoped, for the reason Unsave removes rather than hides: a soft-deleted
// copy stays addressable by its own id. The copies other people kept of a source that named
// the subject are not theirs to lose, and the name in them is not theirs to keep: a copy
// outlives its source's own rewrites, so this is the only statement that reaches those
// words. A title is prose and takes the kit's match rule; a snapshot is a JSON document kept
// as text and is rewritten as a document, inside the text its strings decode to, so the copy
// it leaves still parses. Both run inside the tenant row-level security already bounded, so
// neither names tenant_id, and neither says anything the keeper did not ask it to. A rewrite
// moves the row's own instant; the copy's revision stays at the one value the table allows
// it, because no caller ever quoted a version of a snapshot's words.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	if err := tx.DB().WithContext(ctx).Unscoped().Where("person_id = ?", s.ID).Delete(&row{}).Error; err != nil {
		return err
	}
	// The kit's own match rule, over every copy the tenant can see rather than the
	// ones carrying the subject's id: the person a copy names is not the person who
	// saved it.
	if _, err := personaldata.ScrubTouched(ctx, tx, s, "saved_items", "updated_at",
		[]string{"title"}, ""); err != nil {
		return err
	}
	return scrubSnapshots(ctx, tx, s)
}
