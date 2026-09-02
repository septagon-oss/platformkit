package httpx

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

// Register mounts an operation together with the authorization it declares. It
// is the only way a module registers a handler, and the declaration is a
// parameter rather than a field, so an operation cannot be written without one.
//
// The declaration is written into op.Extensions before huma copies the
// operation, so the recorder, the OpenAPI document and the request-time
// middleware all read the same value under AuthExtension.
func Register[I, O any](api *API, op huma.Operation, auth Auth, handler func(context.Context, *I) (*O, error)) {
	if !auth.declared() {
		panic("httpx.Register: " + describe(&op) + " was passed the zero Auth; use Permission, Public or SignedIn")
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions[AuthExtension] = auth
	huma.Register(api, op, handler)
}
