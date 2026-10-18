package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type operationInput struct {
	Ref string `path:"ref"`
}
type operationResponse struct {
	Status       int
	CacheControl string `header:"Cache-Control"`
	RetryAfter   string `header:"Retry-After"`
	Body         idempotency.Receipt
}

func mountOperations(api *httpx.API, clock func() time.Time) {
	httpx.Register(api.Surfaces("").App, huma.Operation{
		OperationID: "app-operation-outcome", Method: http.MethodGet, Path: "/operations/me/{ref}",
		Summary: "Read an operation started by the current person", Tags: []string{"kernel"}, Errors: []int{400, 404, 503},
	}, httpx.SignedIn(), func(ctx context.Context, in *operationInput) (*operationResponse, error) {
		ref, err := uuid.Parse(in.Ref)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid operation reference")
		}
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, huma.Error503ServiceUnavailable("operation store unavailable")
		}
		p, _ := tenancy.PrincipalFrom(ctx)
		r, err := idempotency.PostgresOperations().Get(ctx, tx, ref, p.UserID, clock())
		if errors.Is(err, crud.ErrNotFound) {
			return nil, huma.Error404NotFound("operation not found")
		}
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("operation store unavailable")
		}
		response := &operationResponse{Status: http.StatusOK, CacheControl: "private, no-store", Body: r}
		if r.Outcome == "" {
			response.Status = http.StatusAccepted
			response.RetryAfter = "1"
		}
		return response, nil
	})
}
