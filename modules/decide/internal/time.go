package internal

import (
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
)

func utc(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return new(value.UTC())
}

func utcBase(b *entity.Base) {
	b.CreatedAt, b.UpdatedAt, b.DeletedAt = b.CreatedAt.UTC(), b.UpdatedAt.UTC(), utc(b.DeletedAt)
}

func utcQuestion(q *contracts.Question) {
	utcBase(&q.Base)
	q.ClosesAt, q.ClosedAt, q.InviteExpiresAt = utc(q.ClosesAt), utc(q.ClosedAt), utc(q.InviteExpiresAt)
}

// UTC decoding keeps immediate and replayed replies identical across drivers.
func utcSnapshot(s *contracts.Snapshot) {
	utcQuestion(&s.Question)
	for i := range s.Options {
		o := &s.Options[i]
		utcBase(&o.Base)
		o.RemovedAt, o.FirstMatchedAt = utc(o.RemovedAt), utc(o.FirstMatchedAt)
	}
	for i := range s.Participants {
		p := &s.Participants[i]
		utcBase(&p.Base)
		p.JoinedAt = p.JoinedAt.UTC()
	}
	for i := range s.Votes {
		v := &s.Votes[i]
		utcBase(&v.Base)
		v.At = v.At.UTC()
	}
}
