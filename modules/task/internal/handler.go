package internal

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// RegisterRoutes mounts the three lifecycle commands on the same resource the
// Spec mounts the five CRUD routes on.
//
// They are routes rather than fields of a PATCH because each is a rule about
// the state the task is in and each publishes an event: a caller who could
// write status="resolved" through the generic update would close the loop with
// no resolution time and tell nobody. spec.Immutable is the other half of that
// argument — it refuses those fields at the PATCH — and rest.Command is the
// part all three commands share, so what is written here is only what each
// command is.
func RegisterRoutes(api *httpx.API, spec rest.Spec[*contracts.Task], svc contracts.Service) {
	rest.Command(api, spec, "assign",
		"Assign a task", "Makes somebody responsible, and acknowledges an open task. Assigning the same person again changes nothing.",
		[]string{contracts.EventAssigned},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in assignBody) (*contracts.Task, error) {
			return svc.Assign(ctx, tx, id, in.AssigneeID)
		})

	rest.Command(api, spec, "resolve",
		"Resolve a task", "Closes the loop. Repeating it with the same resolution changes nothing; a different one is refused.",
		[]string{contracts.EventResolved},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in resolveBody) (*contracts.Task, error) {
			return svc.Resolve(ctx, tx, id, in.Resolution)
		})

	rest.Command(api, spec, "check-sla",
		"Check a task's SLA", "Records a breach if the deadline has passed with the task unresolved. Records at most one.",
		[]string{contracts.EventSLABreached},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.Task, error) {
			return svc.CheckSLA(ctx, tx, id)
		})
}

// assignBody and resolveBody are the arguments of the two commands that take
// one. check-sla takes none, so its body is the empty struct and a caller sends
// no body at all.
type assignBody struct {
	AssigneeID uuid.UUID `json:"assigneeId" format:"uuid" doc:"The user who becomes responsible"`
}

type resolveBody struct {
	Resolution string `json:"resolution,omitempty" maxLength:"4000" doc:"How the task was resolved"`
}
