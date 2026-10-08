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

// personalData is the work record. A task assigned to a person is somebody else's row —
// deleting it would destroy their work — so the row and its assignee_id stay and the
// free text goes: the description and the resolution are where somebody wrote down this
// person's name and what they had done, which is exactly the part Article 17 reaches.
type personalData struct{}

// PersonalData is this module's contribution to a person's export and erasure.
func PersonalData() personaldata.Part { return personaldata.Part{Module: "task", Data: personalData{}} }

type taskPart struct {
	Tasks []taskRow `json:"tasks"`
}

type taskRow struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Priority    string     `json:"priority"`
	Source      string     `json:"source"`
	SourceRef   string     `json:"sourceRef"`
	DueAt       *time.Time `json:"dueAt,omitempty"`
	SLADeadline *time.Time `json:"slaDeadline,omitempty"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
	Resolution  string     `json:"resolution"`
	CreatedAt   time.Time  `json:"createdAt"`
}

func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant],
	s personaldata.Subject) (json.RawMessage, error) {
	var rows []taskRow
	err := tx.DB().WithContext(ctx).Table("tasks").
		Select("id, title, description, status, priority, source, source_ref, due_at, "+
			"sla_deadline, resolved_at, resolution, created_at").
		Where("assignee_id = ?", s.ID).Order("created_at, id").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("task: export %s's tasks: %w", s.ID, err)
	}
	return json.Marshal(taskPart{Tasks: rows})
}

// Erase scrubs title, description and resolution across the tenant's rows: the person a
// task is *about* is not necessarily its assignee, and the note that names them is the
// thing they asked for. What it cannot do is read a description that identifies somebody
// by describing them, and ADR 0020 says so rather than claiming otherwise.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	_, err := personaldata.Scrub(ctx, tx, s, "tasks",
		[]string{"title", "description", "resolution"}, "")
	return err
}
