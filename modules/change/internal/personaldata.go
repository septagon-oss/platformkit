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

// personalData is a decision somebody took about a record. The proposal is the audit
// shape of "who asked, who said yes", so proposer and reviewer keep their ids and the
// sentence written beside them does not.
type personalData struct{}

// PersonalData is this module's contribution to a person's export and erasure.
func PersonalData() personaldata.Part {
	return personaldata.Part{Module: "change", Data: personalData{}}
}

type changePart struct {
	Proposals []proposalRow `json:"proposals"`
}

type proposalRow struct {
	ID            uuid.UUID `json:"id"`
	SubjectModule string    `json:"subjectModule"`
	SubjectEntity string    `json:"subjectEntity"`
	SubjectID     string    `json:"subjectId"`
	BaseRevision  int64     `json:"baseRevision"`
	Summary       string    `json:"summary"`
	Comment       string    `json:"comment"`
	Diff          string    `json:"diff"`
	// Named as change.Proposal names them, because this shape is this module's and
	// the field a proposal carries is `state`, not a second word for the same thing,
	// and the moment it was decided is `reviewed_at` (modules/change/contracts/
	// change.go:115-128). An earlier revision invented "status" and "decidedAt",
	// asked the database for two columns it does not have, and every export of a
	// person died on `column "status" does not exist`.
	State      string     `json:"state"`
	Verdict    string     `json:"verdict"`
	Role       string     `json:"role"`
	ProposedBy string     `json:"proposedBy"`
	ReviewedBy string     `json:"reviewedBy,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ReviewedAt *time.Time `json:"reviewedAt,omitempty"`
}

func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant],
	s personaldata.Subject) (json.RawMessage, error) {
	var rows []proposalRow
	err := tx.DB().WithContext(ctx).Table("change_proposals").
		Select("id, subject_module, subject_entity, subject_id::text AS subject_id, base_revision, "+
			"summary, comment, diff, state, verdict, proposer::text AS proposed_by, reviewer::text AS reviewed_by, "+
			"created_at, reviewed_at, 'proposed' AS role").
		Where("proposer = ? OR reviewer = ?", s.ID, s.ID).Order("created_at, id").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("change: export %s's proposals: %w", s.ID, err)
	}
	return json.Marshal(changePart{Proposals: rows})
}

// Erase scrubs the summary, the comment and the diff of every proposal the tenant can
// see. diff is a text column holding a JSON document, and the scrub is a text operation
// on it, which is why the claim files it as Text and not as a document: an identifier
// inside a JSON string is still the person's identifier, and replacing it inside the
// serialisation leaves the document parseable because the token it replaces is text with
// no quotes in it.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	_, err := personaldata.Scrub(ctx, tx, s, "change_proposals",
		[]string{"summary", "comment", "diff"}, "")
	return err
}
