package internal

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// RegisterRoutes mounts the three lifecycle commands under path, the same path
// the Spec mounts the five CRUD routes on.
//
// They are routes rather than fields of a PATCH because each is a rule about
// the state the task is in and each publishes an event: a caller who could
// write status="resolved" through the generic update would close the loop with
// no resolution time and tell nobody.
func RegisterRoutes(api *httpx.API, svc contracts.Service, path string) {
	command(api, path, "assign", contracts.EventAssigned,
		"Assign a task", "Makes somebody responsible, and acknowledges an open task. Assigning the same person again changes nothing.",
		func(ctx context.Context, tx db.Tx[db.Tenant], in *assignInput) (*contracts.Task, error) {
			return svc.Assign(ctx, tx, in.ID, in.Body.AssigneeID)
		})

	command(api, path, "resolve", contracts.EventResolved,
		"Resolve a task", "Closes the loop. Repeating it with the same resolution changes nothing; a different one is refused.",
		func(ctx context.Context, tx db.Tx[db.Tenant], in *resolveInput) (*contracts.Task, error) {
			return svc.Resolve(ctx, tx, in.ID, in.Body.Resolution)
		})

	command(api, path, "check-sla", contracts.EventSLABreached,
		"Check a task's SLA", "Records a breach if the deadline has passed with the task unresolved. Records at most one.",
		func(ctx context.Context, tx db.Tx[db.Tenant], in *idInput) (*contracts.Task, error) {
			return svc.CheckSLA(ctx, tx, in.ID)
		})
}

// command registers one POST {path}/{id}/{verb}: the same permission, the
// request's transaction, and kit/crud's mapping — 404 for a task this tenant
// does not have, 409 for a state that refuses the command, 422 for an argument
// it refuses — so the lifecycle routes and the CRUD routes on one resource
// cannot disagree about what a status code means.
func command[I any](api *httpx.API, path, verb, event, summary, description string,
	run func(context.Context, db.Tx[db.Tenant], *I) (*contracts.Task, error)) {
	httpx.Register(api, huma.Operation{
		OperationID: "task-task-" + verb,
		Method:      http.MethodPost,
		Path:        path + "/{id}/" + verb,
		Summary:     summary,
		Description: description,
		Tags:        []string{"task"},
		Errors:      []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
		// The same extension kit/crud writes and kit/app's boot gate reads, so
		// these events are in the OpenAPI document and in the gate for the same
		// reason the authorization declaration is.
		Extensions: map[string]any{httpx.EventsExtension: []string{event}},
	}, httpx.Permission(contracts.PermissionTaskUpdate), func(ctx context.Context, in *I) (*taskOutput, error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
		}
		task, err := run(ctx, tx, in)
		if err != nil {
			return nil, crud.Fault(err)
		}
		return &taskOutput{Body: task}, nil
	})
}

type idInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The task's id"`
}

type assignInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"The task's id"`
	Body struct {
		AssigneeID uuid.UUID `json:"assigneeId" format:"uuid" doc:"The user who becomes responsible"`
	}
}

type resolveInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"The task's id"`
	Body struct {
		Resolution string `json:"resolution,omitempty" maxLength:"4000" doc:"How the task was resolved"`
	}
}

// taskOutput is the task as it stands after the command, which is what every
// one of the three returns.
type taskOutput struct {
	Body *contracts.Task
}
