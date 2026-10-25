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

// Erase answers for the subject twice, because one retained copy is two people's
// thing at once. The copies the subject kept are theirs and go outright: a scrub
// would leave a row naming a person, a source key and a captured document, which
// is what the request asked to stop holding. The copies other people kept of a
// source that named the subject are not theirs to lose, and the subject's name in
// them is not theirs to keep — a copy is written to survive its source's later
// rewrites, so this scrub is the only statement that reaches those words. Both
// run in the caller's transaction inside the tenant row-level security already
// bounded, so neither names tenant_id, and neither announces anything: an erasure
// is not a removal the keeper asked for (README §7). The release of a retained
// file's use arrives with the file ledger this composition does not yet wire
// (README §8): a copy written today names a file and records no use for it, so
// there is no use here to release.
//
// The delete is unscoped for the reason Unsave removes rather than hides — a
// soft-deleted copy stays addressable by its own id. The scrub moves the row's own
// instant, because a rewrite is a write and this row names when it last changed.
// What it does not move is the copy's revision, which is fixed at 1 by the table
// itself: the snapshot's words are the copy's own words, and no caller quoted a
// version of them.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	if err := tx.DB().WithContext(ctx).Unscoped().Where("person_id = ?", s.ID).Delete(&row{}).Error; err != nil {
		return err
	}
	// The kit's own match rule, over every copy the tenant can see rather than the
	// ones carrying the subject's id: the person a copy names is not the person who
	// saved it.
	_, err := personaldata.ScrubTouched(ctx, tx, s, "saved_items", "updated_at",
		[]string{"title", "values"}, "")
	return err
}
