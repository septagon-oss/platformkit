package internal

import (
	"cmp"
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
)

func (s *Service) read(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (contracts.Snapshot, contracts.Actor, error) {
	snap, actor, err := s.locked(ctx, tx, id, false)
	if err != nil {
		return contracts.Snapshot{}, contracts.Actor{}, err
	}
	if _, err := contracts.ReadResults(snap, actor, s.now()); err != nil {
		return contracts.Snapshot{}, contracts.Actor{}, err
	}
	return snap, actor, nil
}

func (s *Service) Question(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (contracts.Question, error) {
	snap, _, err := s.read(ctx, tx, id)
	if err != nil {
		return contracts.Question{}, err
	}
	return snap.Question, nil
}

func (s *Service) Questions(ctx context.Context, tx db.Tx[db.Tenant], in contracts.QuestionQuery) (contracts.QuestionPage, error) {
	if err := scope(ctx, tx); err != nil {
		return contracts.QuestionPage{}, err
	}
	actor, err := s.access.Current(ctx, tx, nil)
	if err != nil {
		return contracts.QuestionPage{}, err
	}
	if !actor.Read || actor.PersonID == uuid.Nil {
		return contracts.QuestionPage{}, contracts.ErrDenied
	}
	if in.Offset < 0 || in.Limit < 0 || in.Limit > 200 || (in.Status != "" && in.Status != contracts.StatusOpen && in.Status != contracts.StatusClosed) {
		return contracts.QuestionPage{}, contracts.ErrInvalid
	}
	if in.Limit == 0 {
		in.Limit = 50
	}
	var rows []struct {
		contracts.Question `gorm:"embedded"`
		Total              int64
	}
	err = tx.DB().Raw(`WITH visible AS (
		SELECT q.* FROM decide_questions q WHERE q.deleted_at IS NULL
		AND (q.creator_id = ? OR EXISTS (SELECT 1 FROM decide_participants p
		WHERE p.tenant_id = q.tenant_id AND p.question_id = q.id AND p.person_id = ? AND p.deleted_at IS NULL))
		AND (? = '' OR q.status = ?)
	), page AS (SELECT * FROM visible ORDER BY created_at DESC, id LIMIT ? OFFSET ?)
	SELECT page.*, tally.total FROM (SELECT count(*) AS total FROM visible) tally
	LEFT JOIN page ON true ORDER BY page.created_at DESC, page.id`,
		actor.PersonID, actor.PersonID, in.Status, in.Status, in.Limit, in.Offset).Scan(&rows).Error
	if err != nil {
		return contracts.QuestionPage{}, err
	}
	out := contracts.QuestionPage{Items: []contracts.Question{}}
	for _, row := range rows {
		out.Total = row.Total
		if row.ID != uuid.Nil {
			utcQuestion(&row.Question)
			out.Items = append(out.Items, row.Question)
		}
	}
	return out, nil
}

func (s *Service) Options(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) ([]contracts.Option, error) {
	snap, _, err := s.read(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	rows := slices.DeleteFunc(snap.Options, func(o contracts.Option) bool { return o.RemovedAt != nil })
	slices.SortFunc(rows, func(a, b contracts.Option) int {
		return cmp.Or(cmp.Compare(a.Position, b.Position), compareID(a.ID, b.ID))
	})
	return rows, nil
}

func (s *Service) Participants(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) ([]contracts.Participant, error) {
	snap, _, err := s.read(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(snap.Participants, func(a, b contracts.Participant) int { return compareID(a.PersonID, b.PersonID) })
	return snap.Participants, nil
}

func (s *Service) Results(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (contracts.Results, error) {
	snap, actor, err := s.locked(ctx, tx, id, false)
	if err != nil {
		return contracts.Results{}, err
	}
	return contracts.ReadResults(snap, actor, s.now())
}

func (s *Service) Deck(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (contracts.Deck, error) {
	snap, actor, err := s.locked(ctx, tx, id, false)
	if err != nil {
		return contracts.Deck{}, err
	}
	return contracts.ReadDeck(snap, actor, s.now())
}

func (s *Service) Export(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (contracts.Export, error) {
	snap, actor, err := s.locked(ctx, tx, id, false)
	if err != nil {
		return contracts.Export{}, err
	}
	results, err := contracts.ReadResults(snap, actor, s.now())
	if err != nil {
		return contracts.Export{}, err
	}
	return contracts.Export{Question: snap.Question, Options: snap.Options, Participants: snap.Participants, Votes: snap.Votes, Results: results}, nil
}
