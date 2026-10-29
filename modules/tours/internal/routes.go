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
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tours/contracts"
)

var unavailable = problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")

// RegisterRoutes mounts the four tenant-authenticated tour playback endpoints.
func RegisterRoutes(r *httpx.Router, svc contracts.Service) {
	httpx.Register(r, huma.Operation{OperationID: "tours-offers", Method: http.MethodGet, Path: "/offers", Summary: "List tour offers", Errors: []int{400, 403, 503}}, httpx.Permission(contracts.PermissionRead),
		func(ctx context.Context, _ *offersInput) (*offersOutput, error) {
			if _, err := routeTx(ctx); err != nil {
				return nil, err
			}
			return &offersOutput{Items: []contracts.Run{}}, nil
		})
	httpx.Register(r, huma.Operation{OperationID: "tours-run-create", Method: http.MethodPost, Path: "/runs", Summary: "Start a tour run", Errors: []int{400, 403, 409, 503}}, httpx.Permission(contracts.PermissionRun),
		func(ctx context.Context, in *contracts.Start) (*rest.Item[*contracts.Run], error) {
			tx, err := routeTx(ctx)
			if err != nil {
				return nil, err
			}
			run, err := svc.Start(ctx, tx, *in)
			return &rest.Item[*contracts.Run]{Body: run}, rest.Fault(err)
		})
	httpx.Register(r, huma.Operation{OperationID: "tours-run-read", Method: http.MethodGet, Path: "/runs/{id}", Summary: "Read a tour run", Errors: []int{403, 404, 503}}, httpx.Permission(contracts.PermissionRead),
		func(ctx context.Context, in *runIDInput) (*rest.Item[*contracts.Run], error) {
			tx, err := routeTx(ctx)
			if err != nil {
				return nil, err
			}
			person, ok := tenancy.ActorFrom(ctx)
			if !ok {
				return nil, rest.Fault(crud.ErrNotFound)
			}
			run, err := svc.Get(ctx, tx, person, in.ID)
			return &rest.Item[*contracts.Run]{Body: run}, rest.Fault(err)
		})
	httpx.Register(r, huma.Operation{OperationID: "tours-run-action", Method: http.MethodPost, Path: "/runs/{id}/actions", Summary: "Advance a tour run", Errors: []int{400, 403, 404, 409, 503}}, httpx.Permission(contracts.PermissionRun),
		func(ctx context.Context, in *runActionInput) (*rest.Item[*contracts.Run], error) {
			tx, err := routeTx(ctx)
			if err != nil {
				return nil, err
			}
			person, ok := tenancy.ActorFrom(ctx)
			if !ok {
				return nil, rest.Fault(crud.ErrNotFound)
			}
			run, err := svc.Act(ctx, tx, person, in.ID, in.action())
			return &rest.Item[*contracts.Run]{Body: run}, rest.Fault(err)
		})
}

func routeTx(ctx context.Context) (db.Tx[db.Tenant], error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return db.Tx[db.Tenant]{}, unavailable
	}
	return tx, nil
}

type offersInput struct {
	View string `query:"view" doc:"Composed tour view to inspect"`
}

type offersOutput struct {
	Items []contracts.Run `json:"items"`
}

type runIDInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The run identifier"`
}

type runActionInput struct {
	ID               uuid.UUID `path:"id" format:"uuid" doc:"The run identifier"`
	Kind             string    `json:"action" doc:"The requested transition"`
	Step             string    `json:"step,omitempty" doc:"The current step for step-scoped actions"`
	ExpectedRevision uint64    `json:"expectedRevision" minimum:"1" doc:"The revision the action was based on"`
}

func (in *runActionInput) action() contracts.Action {
	return contracts.Action{Kind: in.Kind, Step: in.Step, ExpectedRevision: in.ExpectedRevision}
}
