package internal

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
)

// Tell writes the snapshot's recipient batch under the kernel's delivery claim.
func Tell(n contracts.Notifier) events.Subscription {
	return events.Subscription{Module: "decide", Name: contracts.EventMatched,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			var payload contracts.Matched
			if err := json.Unmarshal(ev.Payload, &payload); err != nil {
				return err
			}
			if payload.QuestionID == uuid.Nil || payload.OptionID == uuid.Nil || len(payload.Recipients) == 0 {
				return contracts.ErrIntegrity
			}
			return n.Matched(ctx, tx, payload)
		}}
}

// Deadlines materializes due closures; admission already consults the clock.
// Each aggregate has its own transaction, so one failure cannot undo its peers.
func (s *Service) Deadlines(tenants jobs.TenantLister, every time.Duration) jobs.Job {
	return jobs.Job{Name: "decide-deadlines", Every: every, Run: func(ctx context.Context, conn *db.Conn) error {
		return jobs.PerTenant(ctx, conn, tenants, func(ctx context.Context, conn *db.Conn, _ tenancy.Tenant) error {
			var ids []uuid.UUID
			if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				return tx.DB().Model(&contracts.Question{}).Where("status = ? AND closes_at <= ? AND deleted_at IS NULL", contracts.StatusOpen, s.now()).
					Order("closes_at, id").Limit(500).Pluck("id", &ids).Error
			}); err != nil {
				return err
			}
			var failures []error
			for _, id := range ids {
				err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					q, err := crud.GetForUpdate[*contracts.Question](tx, id)
					if err != nil {
						return err
					}
					at := s.now()
					if q.Status != contracts.StatusOpen || q.ClosesAt == nil || at.Before(*q.ClosesAt) {
						return nil
					}
					_, err = closeQuestion(ctx, tx, *q, nil, "deadline", at)
					return err
				})
				if err != nil {
					failures = append(failures, err)
				}
			}
			return errors.Join(failures...)
		})
	}}
}
