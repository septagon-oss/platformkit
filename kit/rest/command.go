package rest

// command.go is the command with no row in the path. Command — POST
// {Path}/{id}/{verb} — is in rest.go, beside the five routes it belongs with.

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// CollectionCommand registers one route on a Spec's collection: POST
// {Path}/{verb}, guarded by the Spec's Write permission, taking the request's
// transaction, declaring the events it publishes, and answering failures with
// the same mapping every other route here uses.
//
// It is Command without the id, and the id is the whole difference: a command
// about a row somebody names — resolve this task — is Command; a command about
// the collection, where the row is what the command produces or finds, is this.
// Redeeming a code is the example that asked for it: the caller knows the code
// and not the row it belongs to, so a route with {id} in it would be asking
// them for the answer.
//
// I is the request body and it is optional, so a command that takes no
// arguments is a POST with no body at all and run is handed the zero I. T is
// what the caller gets back, which for a command that creates something is the
// thing it created.
func CollectionCommand[I any, T crud.Entity](api *httpx.API, spec Spec[T], verb, summary, description string, events []string,
	run func(ctx context.Context, tx db.Tx[db.Tenant], in I) (T, error),
) {
	op := huma.Operation{
		OperationID: spec.Module + "-" + spec.Entity + "-" + verb,
		Method:      http.MethodPost,
		Path:        strings.TrimSuffix(spec.Path, "/") + "/" + verb,
		Summary:     summary,
		Description: description,
		Tags:        []string{spec.Module},
		Errors:      faults,
	}
	if len(events) > 0 {
		op.Extensions = map[string]any{httpx.EventsExtension: events}
	}
	httpx.Register(api, op, spec.writeAuth(),
		func(ctx context.Context, in *collectionInput[I]) (*Item[T], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			var body I
			if in.Body != nil {
				body = *in.Body
			}
			e, err := run(ctx, tx, body)
			if err != nil {
				return nil, Fault(err)
			}
			return &Item[T]{Body: e}, nil
		})
}

// collectionInput is a command's body and nothing else. The body is a pointer
// for the reason commandInput's is: huma reads a struct body as required, and
// "{}" is not something a caller should have to send to say nothing.
type collectionInput[I any] struct {
	Body *I
}
