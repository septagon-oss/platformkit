package rest

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// OperationOptions controls a typed operation's response. An empty CacheControl
// leaves caching to the existing HTTP policy; private projections should declare
// "private, no-store" even when the operation admits anonymous requests.
type OperationOptions struct {
	CacheControl string
}

// Operation mounts a typed service projection through the existing HTTP gates.
// The caller owns the operation, including its method, path, events and status,
// and the callback owns domain authorization and the public response shape.
// Unlike Command, it does not declare an entity resource or generated action.
// The callback receives the request's tenant transaction and principal ID; a
// Public operation may receive uuid.Nil. SignedIn and Permission still require
// identity through httpx.Register. Returning an error rolls back the transaction.
func Operation[I, O any](r *httpx.Router, op huma.Operation, access httpx.Auth,
	run func(context.Context, db.Tx[db.Tenant], uuid.UUID, *I) (O, error), opts OperationOptions,
) {
	httpx.Register(r, op, access, func(ctx context.Context, in *I) (*operationResponse[O], error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		principal, _ := tenancy.PrincipalFrom(ctx)
		out, err := run(ctx, tx, principal.UserID, in)
		if err != nil {
			return nil, Fault(err)
		}
		return &operationResponse[O]{Body: out, CacheControl: opts.CacheControl}, nil
	})
}

type operationResponse[T any] struct {
	CacheControl string `header:"Cache-Control"`
	Body         T
}
