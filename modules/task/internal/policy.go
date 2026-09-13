package internal

import (
	"context"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

func (s *Service) authorize(ctx context.Context, tx db.Tx[db.Tenant], task *contracts.Task, action string, assignee uuid.UUID) error {
	if s.Policy == nil {
		return nil // Compositions without an external policy retain their declared grants.
	}
	principal, ok := tenancy.PrincipalFrom(ctx)
	if !ok || principal.UserID == uuid.Nil {
		return tenancy.ErrPolicyDenied
	}
	attributes := map[string]any{"status": task.Status, "priority": task.Priority, "assignee_id": ""}
	if task.AssigneeID != nil {
		attributes["assignee_id"] = task.AssigneeID.String()
	}
	if assignee != uuid.Nil {
		attributes["requested_assignee_id"] = assignee.String()
	}
	_, err := tenancy.RequirePolicy(ctx, s.Policy, tenancy.PolicyRequest{
		Tenant:   db.TenantOf(tx),
		Actor:    tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: principal.UserID.String()},
		Action:   action,
		Resource: tenancy.PolicyResource{TenantID: task.TenantID, Kind: "task", ID: task.ID.String(), Attributes: attributes},
	})
	return err
}
