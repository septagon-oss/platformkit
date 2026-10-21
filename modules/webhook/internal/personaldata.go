// This file is this module's answer to one contract: personaldata.Data (ADR 0020).
//
// Export is a read of the rows this module owns about one person, in the shape this
// module's own contracts use. Erase removes what those rows *say*. Both run inside the
// caller's transaction, inside the tenant row-level security already bounded, so
// neither names tenant_id.
//
// It is a contributor and never a command: no code in this module calls it. The module
// that composes the parts (modules/subject) does, and the kernel reads the same
// declaration off the manifest to refuse a column this file does not scrub.
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

// personalData is the endpoint ledger. An endpoint is a tenant's machine, not a
// person's record, so Erase keeps every row — withdrawing a customer's subscription
// because one of its administrators left is a write that takes something away — and
// removes the sentences somebody wrote about why it stopped. Export answers for the
// endpoints this person registered, which is the half of this module that is about
// them; the delivery log is addressed to an endpoint and not to a person, so its rows
// are scrubbed for an identifier and never handed out as the person's own.
type personalData struct{}

// PersonalData is this module's contribution to a person's export and erasure.
func PersonalData() personaldata.Part {
	return personaldata.Part{Module: "webhook", Data: personalData{}}
}

type webhookPart struct {
	Endpoints []endpointRow `json:"endpoints"`
}

type endpointRow struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	URL  string    `json:"url"`
	// Events arrives as the JSON array the column already is: the column is text[],
	// and naming its spelling in this document is this module's, not the driver's.
	Events         json.RawMessage `json:"events"`
	Status         string          `json:"status"`
	DisabledReason string          `json:"disabledReason"`
	CreatedAt      time.Time       `json:"createdAt"`
	DisabledAt     *time.Time      `json:"disabledAt,omitempty"`
	LastSuccessAt  *time.Time      `json:"lastSuccessAt,omitempty"`
}

func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant],
	s personaldata.Subject) (json.RawMessage, error) {
	var rows []endpointRow
	// No secret, and none is reachable here: the cleartext lives in
	// webhook_endpoint_secrets and is opened only inside one signing attempt.
	err := tx.DB().WithContext(ctx).Table("webhook_endpoints").
		Select("id, name, url, to_json(events) AS events, status, disabled_reason, "+
			"created_at, disabled_at, last_success_at").
		Where("created_by = ?", s.ID).Order("created_at, id").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("webhook: export %s's endpoints: %w", s.ID, err)
	}
	return json.Marshal(webhookPart{Endpoints: rows})
}

// Erase scrubs the columns somebody can fill with words: an endpoint's label, the reason
// an endpoint was disabled, the reason an attempt failed, and the payload the platform
// POSTed, which is the event's own document and can carry this person's address verbatim.
// The payload is a jsonb column, so it takes ScrubJSON's cast and the refusal that comes
// with it: a stored document that will not parse is a row an erasure cannot certify.
//
// `name` is the label an operator types, and the person whose subscription the endpoint
// tracks is the likeliest thing to type — "Jo Jones <jo.jones@example.com>" is a label
// that reads well until the erasure reaches it. It is a copy of the identifier in the
// same sense a task title is, so it is scrubbed. `url` is not: rewriting a destination
// would break the delivery it is the destination for, which is the write that takes
// something away rather than the one that finds nothing.
//
// `created_by` stays. It is the trail's own kind of attribution — the same account
// modules/audit keeps its actor — and the person it points at is the anonymised row the
// erasure leaves behind, not the address it just removed.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	if _, err := personaldata.Scrub(ctx, tx, s, "webhook_endpoints",
		[]string{"name", "disabled_reason"}, ""); err != nil {
		return err
	}
	if _, err := personaldata.Scrub(ctx, tx, s, "webhook_deliveries",
		[]string{"reason"}, ""); err != nil {
		return err
	}
	_, err := personaldata.ScrubJSON(ctx, tx, s, "webhook_deliveries", []string{"payload"}, "")
	return err
}
