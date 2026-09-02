package internal

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
)

// path is the collection, as modules/tenant spells its own.
const path = "/api/v1/audit/events"

// faults are the statuses these two answer with: no 409 and no 422, because
// nothing here writes. unavailable is the answer when the request has no
// transaction, whose cause the middleware has already logged.
var (
	faults      = []int{http.StatusNotFound, http.StatusServiceUnavailable}
	unavailable = problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
)

// RegisterRoutes mounts the two routes an append-only trail has. They are
// written by hand rather than mounted from a rest.Spec, and that is the shape
// of this module rather than an omission: a Spec is five routes and three of
// them write. A trail nobody can create, update or delete a row in is not a
// resource with three operations turned off — it is a different thing, and
// saying so here is cheaper than a Spec with three holes in it. Both answer
// through kit/rest's mapping, so a 404 means what it means everywhere.
func RegisterRoutes(api *httpx.API, svc contracts.Service) {
	httpx.Register(api, huma.Operation{
		OperationID: "audit-event-list",
		Method:      http.MethodGet,
		Path:        path,
		Summary:     "List the audit trail",
		Description: "Every event this tenant's modules published, newest first. " +
			"Filterable by name, by the user who caused it, and by when it happened.",
		Tags:   []string{"audit"},
		Errors: faults,
	}, httpx.Permission(contracts.PermissionAuditRead),
		func(ctx context.Context, in *listInput) (*listOutput, error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, unavailable
			}
			items, total, err := svc.List(ctx, tx, contracts.Query{
				Name: in.Name, Actor: in.Actor, Since: in.Since, Until: in.Until,
				Limit: in.Limit, Offset: in.Offset,
			})
			if err != nil {
				return nil, rest.Fault(err)
			}
			out := &listOutput{}
			out.Body.Items, out.Body.Total = items, total
			out.Body.Limit, out.Body.Offset = in.Limit, in.Offset
			return out, nil
		})

	httpx.Register(api, huma.Operation{
		OperationID: "audit-event-read",
		Method:      http.MethodGet,
		Path:        path + "/{id}",
		Summary:     "Read one audit event",
		Tags:        []string{"audit"},
		Errors:      faults,
	}, httpx.Permission(contracts.PermissionAuditRead),
		func(ctx context.Context, in *idInput) (*itemOutput, error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, unavailable
			}
			row, err := svc.Get(ctx, tx, in.ID)
			return &itemOutput{Body: row}, rest.Fault(err)
		})
}

type idInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The trail row's id"`
}

// listInput is the page and the three filters. They are separate query
// parameters rather than kit/rest's repeated field:value because two of them
// are ranges, which that syntax has no operator for.
type listInput struct {
	Name   string    `query:"name" doc:"Only events with this name" example:"task.task.created"`
	Actor  uuid.UUID `query:"actor" format:"uuid" doc:"Only events this user caused"`
	Since  time.Time `query:"since" doc:"Only events at or after this instant"`
	Until  time.Time `query:"until" doc:"Only events before this instant"`
	Limit  int       `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Rows per page"`
	Offset int       `query:"offset" minimum:"0" doc:"Rows to skip"`
}

type itemOutput struct {
	Body *contracts.Event
}

type listOutput struct {
	Body struct {
		Items  []*contracts.Event `json:"items"`
		Total  int64              `json:"total"`
		Limit  int                `json:"limit"`
		Offset int                `json:"offset"`
	}
}
