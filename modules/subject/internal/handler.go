// The two doors as an HTTP caller reaches them.
//
// Five routes, all written by hand rather than mounted from a `rest.Spec`, and the reason
// is modules/audit's: a Spec is five routes and three of them write, which describes a
// resource somebody creates, updates and deletes. A person's export request is none of
// those — it is a request that only ever moves forward, and a Spec that could PATCH its
// `state` would be an API that lets anyone mark an erasure as done. So the operations are
// named, and what they do to the state is in the handler that does it.
//
// Note which of the two declares no permission: the person's own export. Article 15 is a
// right and cannot require a grant a person might not hold, so that route declares
// `httpx.SignedIn` and takes the subject from the session rather than the body
// (ADR 0020 §7's first row). Its sibling, which asks for anybody's copy, declares
// `subject:export`, and the erasure declares `subject:erase`.
package internal

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/subject/contracts"
)

// faults are the statuses these answer with. No 409 and no 422 arrives from these handlers
// today: the refusals they make are "no such request" and "no bundle yet", and a status
// this door has never answered is not one it declares.
var faults = []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable}

// unavailable is the answer when the request has no transaction, whose cause the
// middleware already logged.
var unavailable = problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")

// RegisterRoutes mounts the doors. `files` is the file module, which is where a bundle's
// bytes live and the only way to stream them back out.
func RegisterRoutes(r httpx.Surfaces, svc contracts.Service, files filecontracts.Service) {
	httpx.Register(r.App, huma.Operation{
		OperationID: "subject-export-request-mine",
		Method:      http.MethodPost,
		Path:        "/exports",
		Summary:     "Ask for a copy of your own data",
		Description: "Files one export request for the signed-in person and answers it immediately with the " +
			"request row: the bundle is produced by the worker, and this row is how you ask where it got to. " +
			"No permission is asked for, because a person's copy is not a grant.",
		Tags:          []string{"subject"},
		DefaultStatus: http.StatusAccepted,
		Errors:        faults,
		Extensions:    map[string]any{httpx.EventsExtension: []string{contracts.EventExportRequested}},
	}, httpx.SignedIn(), func(ctx context.Context, in *mineInput) (*rest.Item[*contracts.Request], error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, unavailable
		}
		actor, ok := tenancy.ActorFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusUnauthorized, "this request carries no identity to export")
		}
		row, err := svc.RequestExport(ctx, tx, contracts.RequestExportInput{Subject: actor, Reason: in.Reason})
		return &rest.Item[*contracts.Request]{Body: row}, fault(err)
	})

	httpx.Register(r.App, huma.Operation{
		OperationID: "subject-export-request-for",
		Method:      http.MethodPost,
		Path:        "/exports/for",
		Summary:     "Ask for a copy of anybody's data",
		Description: "The operator's door: the same request, filed about a person named in the body. Guarded by " +
			"subject:export, because producing somebody else's bundle is producing a copy of their rows.",
		Tags:          []string{"subject"},
		DefaultStatus: http.StatusAccepted,
		Errors:        faults,
		Extensions:    map[string]any{httpx.EventsExtension: []string{contracts.EventExportRequested}},
	}, httpx.Permission(contracts.PermissionExport),
		func(ctx context.Context, in *forInput) (*rest.Item[*contracts.Request], error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, unavailable
			}
			row, err := svc.RequestExport(ctx, tx, contracts.RequestExportInput{Subject: in.Subject, Reason: in.Reason})
			return &rest.Item[*contracts.Request]{Body: row}, fault(err)
		})

	httpx.Register(r.App, huma.Operation{
		OperationID: "subject-export-link-mine",
		Method:      http.MethodPost,
		Path:        "/exports/{id}/link",
		Summary:     "Mint a download link for your own export",
		Description: "Opens the seven-day, single-use door onto a bundle that is ready. Somebody else's request " +
			"is not found. The token appears in this response and in no row, log or event, so this is the one " +
			"answer that carries it.",
		Tags:   []string{"subject"},
		Errors: faults,
	}, httpx.SignedIn(), func(ctx context.Context, in *idInput) (*rest.Item[*contracts.Link], error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, unavailable
		}
		actor, ok := tenancy.ActorFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusUnauthorized, "this request carries no identity")
		}
		link, err := svc.Mint(ctx, tx, in.ID, actor, false)
		return &rest.Item[*contracts.Link]{Body: link}, fault(err)
	})

	httpx.Register(r.App, huma.Operation{
		OperationID: "subject-export-link-for",
		Method:      http.MethodPost,
		Path:        "/exports/for/{id}/link",
		Summary:     "Mint a download link for any export",
		Description: "The operator's share of the same door: a link for a request the caller did not file. " +
			"Guarded by subject:export, and the mint is auditable because the trail records every write here.",
		Tags:   []string{"subject"},
		Errors: faults,
	}, httpx.Permission(contracts.PermissionExport),
		func(ctx context.Context, in *idInput) (*rest.Item[*contracts.Link], error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, unavailable
			}
			actor, _ := tenancy.ActorFrom(ctx)
			link, err := svc.Mint(ctx, tx, in.ID, actor, true)
			return &rest.Item[*contracts.Link]{Body: link}, fault(err)
		})

	httpx.Register(r.App, huma.Operation{
		OperationID: "subject-erase-request",
		Method:      http.MethodPost,
		Path:        "/erasures",
		Summary:     "Erase a person's data",
		Description: "Asks every owner of a person's rows for its own, in module-name order, and files the " +
			"receipt. The confirm field is that person's address, repeated: an irreversible act gets the " +
			"one-keystroke refusal. A refused erasure writes nothing at all, including the request row.",
		Tags:       []string{"subject"},
		Errors:     faults,
		Extensions: map[string]any{httpx.EventsExtension: []string{contracts.EventErased}},
	}, httpx.Permission(contracts.PermissionErase),
		func(ctx context.Context, in *eraseInput) (*rest.Item[*contracts.Erasure], error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, unavailable
			}
			_, receipt, err := svc.RequestErase(ctx, tx, contracts.EraseInput{Subject: in.Subject, Confirm: in.Confirm})
			return &rest.Item[*contracts.Erasure]{Body: receipt}, fault(err)
		})

	// The bundle door: the seven-day link, on the public surface, because the person who
	// asked for a copy has to be able to open it at three in the morning on a phone with an
	// expired session and no intention of signing in again. Nothing else is public here: the
	// token is the credential, and it is hashed at rest, single-use and seven days old at
	// most (ADR 0020 §6). A wrong, spent or expired token is a 404 that says nothing more.
	httpx.Register(r.Public, huma.Operation{
		OperationID: "subject-export-bundle",
		Method:      http.MethodGet,
		Path:        "/bundles/{token}",
		Summary:     "Download an exported bundle",
		Description: "The bytes of one person's export, opened by the single-use token that was minted for it. " +
			"An expired, used or forged token gets the same answer, and the answer is the one a stranger gets.",
		Tags:   []string{"subject"},
		Errors: []int{http.StatusNotFound, http.StatusServiceUnavailable},
	}, httpx.Public(), func(ctx context.Context, in *tokenInput) (*huma.StreamResponse, error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, unavailable
		}
		link, err := svc.Redeem(ctx, tx, in.Token)
		if err != nil {
			if errors.Is(err, contracts.ErrGrant) {
				return nil, problem.NotFound("that link is not open")
			}
			return nil, rest.Fault(err)
		}
		f, body, err := files.Open(ctx, tx, link.File, false)
		if err != nil {
			return nil, rest.Fault(err)
		}
		req, _ := httpx.RequestFrom(ctx)
		return filecontracts.ContentResponse(req, f, body, filecontracts.Inline), nil
	})
}

// fault is kit/rest's mapping. The one error this module owns that rest cannot know about
// is ErrGrant, and the only route that can raise it answers 404 beside it: whether a link
// was wrong, spent or too old is not information, and a 410 would tell a forger which of
// their guesses was warm.
func fault(err error) error {
	if err == nil {
		return nil
	}
	return rest.Fault(err)
}

// idInput is one request row, named by the answer that filed it.
type idInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The export request's id"`
}

// mineInput is the body a person may send with their own request: a reason, and nothing
// else. There is no subject field on this route on purpose — the subject of a request made
// by a signed-in person is that person, and a body that could name somebody else would be
// a door onto their rows under a permission that asks for none.
type mineInput struct {
	Reason string `json:"reason" maxLength:"500" doc:"Why you are asking, in your words. Recorded, and it buys nothing."`
}

// forInput is the operator's ask: the person, and the reason.
type forInput struct {
	Subject uuid.UUID `json:"subject" format:"uuid" doc:"The person whose copy is wanted"`
	Reason  string    `json:"reason" maxLength:"500" doc:"Why, recorded on the request"`
}

// eraseInput is the irreversible ask. Confirm is checked against the person's own row, not
// against this body, which is the whole of what it is for.
type eraseInput struct {
	Subject uuid.UUID `json:"subject" format:"uuid" doc:"The person to stop holding data about"`
	Confirm string    `json:"confirm" maxLength:"320" doc:"That person's email address, repeated"`
}

// tokenInput is a minted link.
type tokenInput struct {
	Token string `path:"token" pattern:"^[0-9a-f]{64}$" doc:"The single-use token this link was minted with"`
}
