package events

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
)

// PendingDelivery is transaction-visible intent without a relay publication stamp.
// Publication means transport acceptance, not completion of every subscription.
type PendingDelivery struct {
	EventID, TenantID uuid.UUID
	Name              string
	CreatedAt         time.Time
}

// FailedDelivery identifies a retained terminal subscription outcome. Its cause
// and event payload remain outside the inspection API because they may be private.
type FailedDelivery struct {
	EventID, TenantID uuid.UUID
	Name, Durable     string
	FailedAt          time.Time
}

// DeliveryInspection contains bounded metadata from two sequential reads. Each
// list reflects its own statement; concurrent delivery can change the database
// between them. More reports truncation, never an estimated or exact total.
type DeliveryInspection struct {
	Pending                   []PendingDelivery
	Failures                  []FailedDelivery
	PendingMore, FailuresMore bool
}

// InspectDelivery reads the oldest pending events and latest terminal failures,
// using the caller's explicit system transaction. The caller owns operator
// authorization and cancellation. Limit must be between 1 and 100. No record is
// locked for delivery, retried, acknowledged or changed by this operation.
// Writes already made in tx are visible too; results remain provisional until
// that caller-owned transaction commits. This API does not commit it.
func InspectDelivery(ctx context.Context, tx db.Tx[db.System], limit int) (DeliveryInspection, error) {
	if limit < 1 || limit > 100 || tx.DB() == nil {
		return DeliveryInspection{}, errors.New("events: inspection requires a system transaction and a limit between 1 and 100")
	}
	var result DeliveryInspection
	query := tx.DB().WithContext(ctx)
	if err := query.Raw(`SELECT id AS event_id, tenant_id, name, created_at FROM `+table+`
		WHERE published_at IS NULL ORDER BY created_at, id LIMIT ?`, limit+1).Scan(&result.Pending).Error; err != nil {
		return DeliveryInspection{}, fmt.Errorf("events: inspect pending deliveries: %w", err)
	}
	if err := query.Raw(`SELECT event_id, tenant_id, name, durable, failed_at FROM `+deadLetters+`
		ORDER BY failed_at DESC, event_id, durable LIMIT ?`, limit+1).Scan(&result.Failures).Error; err != nil {
		return DeliveryInspection{}, fmt.Errorf("events: inspect terminal failures: %w", err)
	}
	result.PendingMore, result.FailuresMore = len(result.Pending) > limit, len(result.Failures) > limit
	result.Pending = slices.Clip(result.Pending[:min(len(result.Pending), limit)])
	result.Failures = slices.Clip(result.Failures[:min(len(result.Failures), limit)])
	return result, nil
}
