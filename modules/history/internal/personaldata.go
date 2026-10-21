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

// personalData is the content trail. It is a version of somebody else's record rather
// than a row of the writer's own, so the writer is filed as a person the row is about
// and not as the row's subject, and the erasure keeps the id for the same reason the
// audit trail keeps its actor: "which version is this" has to keep answering after the
// name is gone. What goes is the person's own words inside a version's body — a title,
// a sentence in a description — which is the only thing here a data-protection request
// is ever about (ADR 0020 §4).
//
// refs and changed name fields and files, never a person, so neither is scrubbed: a
// scrub of a column that cannot hold a name rewrites a row for no protection, and this
// table's rows are compared to each other by exactly the fields it does not touch.
type personalData struct{}

// PersonalData is this module's contribution to a person's export and erasure.
func PersonalData() personaldata.Part {
	return personaldata.Part{Module: "history", Data: personalData{}}
}

type historyPart struct {
	Versions []versionRow `json:"versions"`
}

type versionRow struct {
	Module   string          `json:"module"`
	Entity   string          `json:"entity"`
	RecordID string          `json:"recordId"`
	Locale   string          `json:"locale"`
	Version  int64           `json:"version"`
	Fields   json.RawMessage `json:"fields"`
	Reason   string          `json:"reason"`
	At       time.Time       `json:"at"`
}

// versionScan is the same row as the driver returns it: a jsonb column arrives as text,
// and selecting straight into json.RawMessage fails the scan. See the same split in
// modules/audit/internal/personaldata.go.
type versionScan struct {
	Module   string
	Entity   string
	RecordID uuid.UUID
	Locale   string
	Version  int64
	Fields   string
	Reason   string
	At       time.Time
}

func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant],
	s personaldata.Subject) (json.RawMessage, error) {
	var scanned []versionScan
	err := tx.DB().WithContext(ctx).Table(table).
		Select("module, entity, record_id, locale, version, fields::text AS fields, reason, committed_at AS at").
		Where("actor = ?", s.ID).Order("committed_at, id").Find(&scanned).Error
	if err != nil {
		return nil, fmt.Errorf("history: export %s's versions: %w", s.ID, err)
	}
	rows := make([]versionRow, len(scanned))
	for i, v := range scanned {
		rows[i] = versionRow{
			Module: v.Module, Entity: v.Entity, RecordID: v.RecordID.String(), Locale: v.Locale,
			Version: v.Version, Fields: json.RawMessage(v.Fields), Reason: v.Reason, At: v.At,
		}
	}
	return json.Marshal(historyPart{Versions: rows})
}

// Erase scrubs every version body in the tenant, not only the ones this person wrote,
// for the reason the other contributors give: the version that repeats an address is
// often the one an administrator saved about them, and it carries somebody else's actor.
//
// The UPDATE is the one this table's own comment excepts. No version is rewritten into
// another value — the row still answers "what did version 4 hold", with the person's
// name replaced by the placeholder their id carries — and a restore of a scrubbed
// version copies the placeholder forward, which is what an erasure that survived its own
// history looks like.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	_, err := personaldata.ScrubJSON(ctx, tx, s, table, []string{"fields"}, "")
	return err
}
