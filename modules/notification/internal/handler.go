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
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// path is the collection, as modules/tenant and modules/audit spell their own.
const path = "/api/v1/notification/notifications"

// anonymous is unreachable — both routes declare SignedIn, so the kernel has
// already refused a caller without a principal — and it is here so that the
// branch reading the principal answers rather than scoping a list to the zero
// uuid. unavailable is the answer when the request has no transaction.
var (
	faults      = []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable}
	anonymous   = problem.New(http.StatusForbidden, "these operations answer for the caller themselves")
	unavailable = problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
)

// RegisterRoutes mounts the two routes a per-recipient resource has.
//
// There is no rest.Spec, and the reason is one sentence: a Spec's list route is
// the whole tenant and these rows are addressed to somebody. Every caller's
// list is a different list, so there is no permission to ask for either —
// SignedIn is the whole of it, and the scoping is that the recipient is the
// principal rather than a parameter.
func RegisterRoutes(api *httpx.API, svc contracts.Service) {
	httpx.Register(api, huma.Operation{
		OperationID: "notification-notification-list",
		Method:      http.MethodGet,
		Path:        path,
		Summary:     "List my notifications",
		Description: "The caller's own notifications, newest first. There is no route that lists anybody else's.",
		Tags:        []string{"notification"},
		Errors:      faults,
	}, httpx.SignedIn(), func(ctx context.Context, in *listInput) (*listOutput, error) {
		tx, me, err := caller(ctx)
		if err != nil {
			return nil, err
		}
		items, total, err := svc.ListFor(ctx, tx, me, crud.Query{Limit: in.Limit, Offset: in.Offset, Sort: in.Sort})
		if err != nil {
			return nil, rest.Fault(err)
		}
		out := &listOutput{}
		out.Body.Items, out.Body.Total = items, total
		out.Body.Limit, out.Body.Offset = in.Limit, in.Offset
		return out, nil
	})

	httpx.Register(api, huma.Operation{
		OperationID: "notification-notification-read",
		Method:      http.MethodPost,
		Path:        path + "/{id}/read",
		Summary:     "Mark one of my notifications read",
		Description: "Marking it read again changes nothing and publishes nothing.",
		Tags:        []string{"notification"},
		Errors:      faults,
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventRead}},
	}, httpx.SignedIn(), func(ctx context.Context, in *idInput) (*itemOutput, error) {
		tx, me, err := caller(ctx)
		if err != nil {
			return nil, err
		}
		row, err := svc.MarkRead(ctx, tx, in.ID, me)
		return &itemOutput{Body: row}, rest.Fault(err)
	})
}

// caller is the request's transaction and the person making it, which both
// routes need and neither may take from a parameter.
func caller(ctx context.Context) (tx db.Tx[db.Tenant], me uuid.UUID, err error) {
	p, ok := httpx.PrincipalFrom(ctx)
	if !ok || p.UserID == uuid.Nil {
		return tx, me, anonymous
	}
	tx, ok = httpx.TxFrom(ctx)
	if !ok {
		return tx, me, unavailable
	}
	return tx, p.UserID, nil
}

type idInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The notification's id"`
}

type listInput struct {
	Limit  int    `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Rows per page"`
	Offset int    `query:"offset" minimum:"0" doc:"Rows to skip"`
	Sort   string `query:"sort" doc:"A field name, or a field name prefixed with - for descending"`
}

type itemOutput struct {
	Body *contracts.Notification
}

type listOutput struct {
	Body struct {
		Items  []*contracts.Notification `json:"items"`
		Total  int64                     `json:"total"`
		Limit  int                       `json:"limit"`
		Offset int                       `json:"offset"`
	}
}
