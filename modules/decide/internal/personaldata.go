package internal

import (
	"context"
	"encoding/json"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
)

type personalData struct{}

// PersonalData contributes authored content and the subject's own participation
// to an authorized caller's export or erasure in its tenant transaction.
func PersonalData() personaldata.Part {
	return personaldata.Part{Module: "decide", Data: personalData{}}
}

type personalRecord struct {
	Questions    []contracts.Question    `json:"questions"`
	Options      []contracts.Option      `json:"options"`
	Participants []contracts.Participant `json:"participants"`
	Votes        []contracts.Vote        `json:"votes"`
}

func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) (json.RawMessage, error) {
	out := personalRecord{}
	q := tx.DB().WithContext(ctx)
	if err := q.Table("decide_questions").Where("creator_id = ?", s.ID).Order("id").Find(&out.Questions).Error; err != nil {
		return nil, err
	}
	if err := q.Table("decide_options").Where("question_id IN (SELECT id FROM decide_questions WHERE creator_id = ?)", s.ID).
		Order("question_id, position, id").Find(&out.Options).Error; err != nil {
		return nil, err
	}
	if err := q.Table("decide_participants").Where("person_id = ?", s.ID).Order("id").Find(&out.Participants).Error; err != nil {
		return nil, err
	}
	if err := q.Table("decide_votes").Where("person_id = ?", s.ID).Order("id").Find(&out.Votes).Error; err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// Erase removes identifying words from the tenant's shared content while
// retaining identities, ballots and outcomes. Changed text advances its row's
// revision so a command based on the old words cannot overwrite the scrub.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	if _, err := personaldata.ScrubCounted(ctx, tx, s, "decide_questions", entity.RevisionField,
		[]string{"title", "description"}, ""); err != nil {
		return err
	}
	_, err := personaldata.ScrubCounted(ctx, tx, s, "decide_options", entity.RevisionField,
		[]string{"title", "note", "link"}, "")
	return err
}
