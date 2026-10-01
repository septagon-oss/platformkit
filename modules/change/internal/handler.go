package internal

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
)

// path is the collection, as modules/audit spells its own. The kernel adds the
// module segment, the surface prefix and the middleware.
const path = "/proposals"

// unavailable is the answer when the request carries no transaction, whose cause
// the middleware has already logged.
var unavailable = problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")

// faults are the statuses these six operations answer with. Both the not-found of
// a row another tenant owns and the conflict of a row that is not in the state this
// command needs are the kernel's own answers, mapped once in kit/rest's Fault.
var faults = []int{
	http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusServiceUnavailable,
}

// RegisterRoutes mounts the two reads and the four commands a proposal has.
//
// They are written by hand rather than mounted from a rest.Spec, and that is the
// shape of this module rather than an omission: a Spec is five routes on a
// collection, three of which write whatever the body says. A proposal whose state
// moves only through four named commands, each with an actor rule of its own, is
// not a resource with three operations turned off. Every operation still declares
// the events it publishes with the same extension rest.Command sets, so the boot
// gate sees a promise the manifest has to keep.
func RegisterRoutes(s httpx.Surfaces, svc contracts.Service) {
	read := httpx.Permission(contracts.PermissionChangeRead)
	propose := httpx.Permission(contracts.PermissionChangePropose)
	decide := httpx.Permission(contracts.PermissionChangeDecide)

	httpx.Register(s.App, huma.Operation{
		OperationID: "change-proposal-list",
		Method:      http.MethodGet,
		Path:        path,
		Summary:     "List proposals",
		Description: "Change proposals, newest first, filterable by state and by the row they are about.",
		Tags:        []string{"change"},
		Errors:      faults,
	}, read, func(ctx context.Context, in *listInput) (*rest.Page[*contracts.Proposal], error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, unavailable
		}
		items, total, err := svc.List(ctx, tx, contracts.Query{
			State: in.State, SubjectModule: in.SubjectModule, SubjectEntity: in.SubjectEntity,
			SubjectID: in.SubjectID, Limit: in.Limit, Offset: in.Offset,
		})
		if err != nil {
			return nil, rest.Fault(err)
		}
		out := &rest.Page[*contracts.Proposal]{}
		out.Body.Items, out.Body.Total = items, total
		out.Body.Limit, out.Body.Offset = in.Limit, in.Offset
		return out, nil
	})

	httpx.Register(s.App, huma.Operation{
		OperationID: "change-proposal-read",
		Method:      http.MethodGet,
		Path:        path + "/{id}",
		Summary:     "Read one proposal",
		Tags:        []string{"change"},
		Errors:      faults,
	}, read, func(ctx context.Context, in *idInput) (*rest.Item[*contracts.Proposal], error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, unavailable
		}
		row, err := svc.Get(ctx, tx, in.ID)
		return &rest.Item[*contracts.Proposal]{Body: row}, rest.Fault(err)
	})

	httpx.Register(s.App, huma.Operation{
		OperationID: "change-proposal-propose",
		Method:      http.MethodPost,
		Path:        path,
		Summary:     "Propose a change",
		Description: "Puts one change to one row forward for somebody else to decide. The proposal records the " +
			"subject's revision it was made against and the digest of the exact bytes; the same change proposed " +
			"again while one is open is the same proposal.",
		Tags:       []string{"change"},
		Errors:     faults,
		Extensions: extension(contracts.EventProposed),
	}, propose, func(ctx context.Context, in *proposeInput) (*rest.Item[*contracts.Proposal], error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, unavailable
		}
		row, err := svc.Propose(ctx, tx, in.Body)
		return &rest.Item[*contracts.Proposal]{Body: row}, rest.Fault(err)
	})

	command(s.App, "withdraw", "Withdraw a proposal",
		"Takes a proposal back. Only the person who put it forward may, and only before it is decided.",
		[]string{contracts.EventWithdrawn}, propose,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in revisionInput) (*contracts.Proposal, error) {
			return svc.Withdraw(ctx, tx, id, in.ExpectedRevision)
		})

	command(s.App, "review", "Decide a proposal",
		"Approves or declines it. The person who proposed it cannot be the one who decides, and the decision "+
			"has to name the revision it was made about.",
		[]string{contracts.EventReviewed}, decide,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in contracts.Review) (*contracts.Proposal, error) {
			return svc.Review(ctx, tx, id, in)
		})

	command(s.App, "apply", "Apply an approved change",
		"Writes the diff to the subject. Refuses unless the proposal is approved, the actor is not its proposer, "+
			"and the subject is still on the revision the diff was made against; applies once, however often it "+
			"is asked.",
		[]string{contracts.EventApplied}, decide,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in revisionInput) (*contracts.Proposal, error) {
			return svc.Apply(ctx, tx, id, in.ExpectedRevision)
		})
}

// command is the shape all three state-moving commands share: a POST under the
// proposal's own address, the actor's transaction out of the request, kit/rest's
// error mapping, and the event it publishes written on the operation where the boot
// gate can read it. B is the command's own request body, so withdraw and review —
// which take different arguments — go through one mount.
func command[B any](r *httpx.Router, verb, summary, description string, publishes []string, auth httpx.Auth,
	run func(context.Context, db.Tx[db.Tenant], uuid.UUID, B) (*contracts.Proposal, error),
) {
	httpx.Register(r, huma.Operation{
		OperationID: "change-proposal-" + verb,
		Method:      http.MethodPost,
		Path:        path + "/{id}/" + verb,
		Summary:     summary,
		Description: description,
		Tags:        []string{"change"},
		Errors:      faults,
		Extensions:  extension(publishes...),
	}, auth, func(ctx context.Context, in *commandInput[B]) (*rest.Item[*contracts.Proposal], error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, unavailable
		}
		row, err := run(ctx, tx, in.ID, in.Body)
		return &rest.Item[*contracts.Proposal]{Body: row}, rest.Fault(err)
	})
}

// extension records what an operation publishes, in the one place the kernel looks.
func extension(names ...string) map[string]any {
	if len(names) == 0 {
		return nil
	}
	return map[string]any{httpx.EventsExtension: names}
}

// The request bodies. A body carries no actor: the person who withdrew, reviewed or
// applied is whoever the credentials said, and an identity a caller could send is
// one a caller could choose.

type proposeInput struct {
	Body contracts.NewProposal `json:"body"`
}

type commandInput[B any] struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"The proposal"`
	Body B         `json:"body"`
}

type revisionInput struct {
	ExpectedRevision int64 `json:"expectedRevision" required:"true" minimum:"1" doc:"The revision this decision was made about"`
}

type idInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The proposal"`
}

type listInput struct {
	State         string    `query:"state" enum:"proposed,approved,declined,withdrawn,applied" doc:"Only proposals in this state"`
	SubjectModule string    `query:"subjectModule" doc:"Only proposals for this module's rows" example:"site"`
	SubjectEntity string    `query:"subjectEntity" doc:"Only proposals for this entity" example:"settings"`
	SubjectID     uuid.UUID `query:"subjectId" format:"uuid" doc:"Only proposals about this row"`
	Limit         int       `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Rows per page"`
	Offset        int       `query:"offset" minimum:"0" doc:"Rows to skip"`
}
