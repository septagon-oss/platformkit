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

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
)

// personalData is the trail. It is the reason the erasure anonymises instead of
// deleting: "what happened to this row" has to keep answering, with the actor as an id
// and the event name intact, because an event name is a contract and not a person
// (ADR 0020 §4). What goes is the address a payload carried verbatim, and the client
// address the row recorded — an IP is personal data, and its removal costs the trail
// nothing it promised. request_id and traceparent stay: they are opaque, and dropping
// them would break "every row this trace touched" for no data-protection gain.
type personalData struct{}

// PersonalData is this module's contribution to a person's export and erasure.
func PersonalData() personaldata.Part {
	return personaldata.Part{Module: "audit", Data: personalData{}}
}

type auditPart struct {
	Events []eventRow `json:"events"`
}

type eventRow struct {
	Name       string          `json:"name"`
	OccurredAt time.Time       `json:"occurredAt"`
	Actor      string          `json:"actor"`
	EventID    string          `json:"eventId"`
	Payload    json.RawMessage `json:"payload"`
	RequestID  *string         `json:"requestId,omitempty"`
}

// eventScan is the same row as it comes back from the driver. json.RawMessage is a
// byte slice the encoder validates, and a jsonb column arrives as a string, so the
// two are not one struct: an earlier revision selected straight into the marshalled
// shape and every export of a person with one audit row died with `unsupported Scan,
// storing driver.Value type string into type *jsontext.Value`. The payload is read as
// the text the database gives and handed on as the document it already is.
type eventScan struct {
	Name       string
	OccurredAt time.Time
	EventID    string
	Payload    string
	RequestID  *string
}

func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant],
	s personaldata.Subject) (json.RawMessage, error) {
	// client_ip is not selected: the address a row recorded belongs to whoever acted,
	// which for a trail about this person is often somebody else, and one person's
	// bundle must not carry another's address. It is the actor's own address that the
	// erasure below removes.
	var scanned []eventScan
	err := tx.DB().WithContext(ctx).Table("audit_events").
		Select("name, occurred_at, event_id::text AS event_id, payload::text AS payload, request_id").
		Where("actor = ?", s.ID).Order("occurred_at, id").Find(&scanned).Error
	if err != nil {
		return nil, fmt.Errorf("audit: export %s's trail: %w", s.ID, err)
	}
	rows := make([]eventRow, len(scanned))
	for i, e := range scanned {
		rows[i] = eventRow{
			Name: e.Name, OccurredAt: e.OccurredAt, Actor: s.ID.String(),
			EventID: e.EventID, Payload: json.RawMessage(e.Payload), RequestID: e.RequestID,
		}
	}
	return json.Marshal(auditPart{Events: rows})
}

// Erase scrubs every payload in the tenant and clears the address of this person's rows.
//
// The scrub is over every row and not only the actor's, because the trail records other
// people's actions *on* this person — an administrator deactivating an account writes
// that account's address into its payload — and a trail that kept those sentences would
// keep exactly what the person asked to stop being findable.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	if _, err := personaldata.ScrubJSON(ctx, tx, s, "audit_events", []string{"payload"}, ""); err != nil {
		return err
	}
	return tx.DB().WithContext(ctx).
		Exec("UPDATE audit_events SET client_ip = NULL WHERE actor = ?", s.ID).Error
}
