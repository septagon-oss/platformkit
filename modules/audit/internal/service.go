// Package internal is every implementation of the audit module. Nothing outside
// modules/audit can import it, which is the compiler enforcing idea 3.
package internal

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
)

// table is the one table, named here and in migrations/000010.
const table = "audit_events"

// Service is the audit trail. It has no fields: everything a command needs
// arrives with the transaction, so one instance serves a request and an event
// handler at once.
type Service struct{}

// NewService returns the trail. module.go constructs it.
func NewService() *Service { return &Service{} }

var _ contracts.Service = (*Service)(nil)

// Record writes one event into the trail. The insert is by hand because the row
// is not a crud.Entity — nothing here is updated or soft-deleted — and because
// the conflict clause is the point: recording is idempotent by the event's own
// id, the second lock behind the kernel's handled table. That one claims each
// delivery of each subscription; this one covers what it cannot, which is an
// operator replaying an outbox row or a handler that failed after writing.
//
// The tenant comes from the transaction and not from ev.TenantID. The two
// always agree, because Consume opened this transaction in the event's own
// tenant; taking it from the transaction anyway is what makes the isolation the
// database's rather than a struct field's.
func (s *Service) Record(_ context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
	var actor any
	if ev.Actor != uuid.Nil {
		actor = ev.Actor
	}
	err := tx.DB().Exec("INSERT INTO "+table+
		" (tenant_id, occurred_at, name, actor, event_id, payload) VALUES (?, ?, ?, ?, ?, ?::jsonb)"+
		" ON CONFLICT (event_id) DO NOTHING",
		db.TenantOf(tx).ID, ev.At, ev.Name, actor, ev.ID, string(ev.Payload)).Error
	if err != nil {
		return fmt.Errorf("audit: record %s: %w", ev.Name, err)
	}
	return nil
}

// List is a page of this tenant's trail, newest first. It is a hand-written
// query and not a crud.List because two of the filters are range comparisons,
// which kit/crud's equalities cannot express — the same reason the SLA sweep
// writes its own. The count and the page come from separate statements, because
// GORM carries clauses forward and a Count that inherited the LIMIT would count
// the page; the order ends in the id, so two rows recorded in one transaction
// neither overlap nor skip between pages.
func (s *Service) List(_ context.Context, tx db.Tx[db.Tenant], q contracts.Query) ([]*contracts.Event, int64, error) {
	build := func() *gorm.DB {
		g := tx.DB().Table(table)
		if q.Name != "" {
			g = g.Where("name = ?", q.Name)
		}
		if q.Actor != uuid.Nil {
			g = g.Where("actor = ?", q.Actor)
		}
		if !q.Since.IsZero() {
			g = g.Where("occurred_at >= ?", q.Since)
		}
		if !q.Until.IsZero() {
			g = g.Where("occurred_at < ?", q.Until)
		}
		return g
	}
	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("audit: count the trail: %w", err)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = crud.DefaultLimit
	}
	var rows []*contracts.Event
	err := build().Order("occurred_at DESC, id").
		Limit(min(limit, crud.MaxLimit)).Offset(max(q.Offset, 0)).Find(&rows).Error
	if err != nil {
		return nil, 0, fmt.Errorf("audit: read the trail: %w", err)
	}
	return rows, total, nil
}

// Get is one row of this tenant's trail. A row another tenant owns is not
// found, which is the only thing the API may say about it.
func (s *Service) Get(_ context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Event, error) {
	var row contracts.Event
	err := tx.DB().Table(table).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, crud.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("audit: read %s: %w", id, err)
	}
	return &row, nil
}
