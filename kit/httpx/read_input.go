package httpx

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// ReadInput is one typed query's generated input/output projection. Run checks
// current access and uses the request tenant transaction; errors have no result.
// The declaring module promises the query has no business mutation or event.
type ReadInput struct {
	Output     []entity.Field
	OutputList bool
	Path       string
	Run        func(context.Context, map[string]any) (any, error)
}

func (a *API) guardReadInput(r Resource) Resource {
	if r.Input != nil {
		input, run := *r.Input, r.Input.Run
		input.Run = func(ctx context.Context, values map[string]any) (any, error) {
			if err := a.mayDeclare(ctx, r.ReadAuth()); err != nil {
				return nil, err
			}
			return run(ctx, values)
		}
		r.Input = &input
	}
	return r
}
