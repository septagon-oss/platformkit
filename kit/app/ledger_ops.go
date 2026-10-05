package app

// ledger_ops.go mounts the installation's two delivery verbs on the control plane:
// move a ledger onto this app's durable, and replay one event. The kernel owns the
// mount because no capability module owns "the sequel to a kernel write": the two
// commands live in kit/events, they are the only two writes in the kernel that
// cross tenants by design, and each is reached today from a machine — the boot, the
// retry job, the relay — with no door an operator can stand at.
//
// Three things the address has to carry, and where each is checked:
//
//   - Never through an app's own routes. httpx.classify puts /ops on SurfaceOps,
//     the surface is served at the installation host and 404s at every tenant host
//     before routing, the ops chain takes a credential and never a workspace
//     session, and the mount is httpx.OperatorPermission, which the kernel refuses
//     at any tenant but the operator's own.
//   - The app it acts in. The body names an app and the body's answer must be this
//     process's own: a composition answers only for itself, and a request that
//     asks `collect`'s process to move `academy`'s ledger would be one process
//     writing a ledger it does not consume. That refusal is immutable for the
//     process — the fix is the other deployment, not a different retry — and it
//     writes and emits nothing.
//   - The actor behind it. events.Replay already refuses a call with nobody behind
//     it; here the principal comes from the credential the surface chain resolved,
//     so the record each verb writes names a person. A move takes the same actor:
//     the operator's sentence travels as the record's requestedBy, and the boot and
//     the job name themselves instead ("boot", the job's own name).
//
// The verbs exist only for a composition that names itself: an app-less deployment
// has no scoped durable to move onto and no app to name in a record, so both routes
// would be doors onto a zero report. The same condition keeps the event declared
// (kernelModule) and the retry job registered (kernelJobs); one rule, three places.

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// The two grants. Both are operator grants, so the roles that hold them belong to
// the installation's own tenant rather than to a customer's — which is what makes
// "the operator of the app this process is" a question with one answer.
const (
	PermissionLedgerMove  = "platformkit:ledger_move"
	PermissionEventReplay = "platformkit:event_replay"
)

// ledgerMoveBody names the app, because the record has to say which app's ledger
// moved rather than leave it to be inferred from whichever process answered. The
// reason is optional: this verb moves rows a machine is about to move again on its
// next tick anyway, so unlike a replay it is not a person's judgement that the
// trail exists to record — the sentence says why now, and nothing more.
type ledgerMoveBody struct {
	App    string `json:"app" required:"true" maxLength:"63" doc:"The app whose ledger to move, as its slug is spelled in nats.app" example:"collect"`
	Reason string `json:"reason,omitempty" maxLength:"512" doc:"Why the move was ordered now rather than on the next tick; the trail records it beside the actor" example:"flipping collect-eu onto its own durable after the rollout"`
}

type ledgerMoveInput struct {
	Body ledgerMoveBody `required:"true"`
}

// ledgerMoveReport is the move's answer, in the same four numbers MoveReport
// carries: a caller that reads `claims: 0` can tell "already moved" from "found
// nothing to move" because Subscriptions and Tenants say what was looked at.
type ledgerMoveReport struct {
	Subscriptions int   `json:"subscriptions" doc:"Distinct durables renamed" example:"3"`
	Claims        int64 `json:"claims" doc:"Handled rows renamed" example:"128"`
	Dead          int64 `json:"dead" doc:"Terminal failure rows renamed" example:"2"`
	Tenants       int64 `json:"tenants" doc:"Tenants whose ledger moved, and therefore got a record" example:"9"`
}

type eventReplayBody struct {
	App     string `json:"app" required:"true" maxLength:"63" doc:"The app this process is; an event is replayed by the composition that consumes it" example:"collect"`
	Durable string `json:"durable,omitempty" maxLength:"255" doc:"Replay for one subscription's durable only; empty replays it for every subscription that claimed the event" example:"collect+task.task.completed"`
	Reason  string `json:"reason" required:"true" maxLength:"512" doc:"Why this delivery is being ordered again; the trail records it beside the actor" example:"the mailer's timeout was fixed; the customer was never told"`
}

type eventReplayInput struct {
	ID   uuid.UUID       `path:"id" format:"uuid" doc:"The outbox row to relay again"`
	Body eventReplayBody `required:"true"`
}

type eventReplayReport struct {
	EventID uuid.UUID `json:"eventId" format:"uuid"`
	Name    string    `json:"name" doc:"The event's name, as its outbox row spells it"`
	Durable string    `json:"durable,omitempty" doc:"The one durable replayed, when the ask named one"`
	Reason  string    `json:"reason" doc:"The sentence the operator gave"`
}

// namedApp is the body's app checked against this process. The slug is parsed
// before it is compared — a body that spells it badly has given an answer that is
// not a slug at all, which is a correctable refusal — and a well-formed slug that
// is not this one is refused as the deployment mistake it is.
func namedApp(own appname.Name, asked string) (string, error) {
	parsed, err := appname.Parse(asked)
	if err != nil {
		return "", problem.New(http.StatusUnprocessableEntity,
			"app: the body's app is not a slug this kernel can name a durable with: "+err.Error()+"; nothing was written")
	}
	if !parsed.Named() || parsed != own {
		return "", problem.New(http.StatusUnprocessableEntity, fmt.Sprintf(
			"app: this process is not app %q; it answers only for %q, and the other app's own deployment is the one that moves its ledger and replays its events; nothing was written",
			parsed.String(), own.String()))
	}
	return parsed.String(), nil
}

// opsConn is the pool the verb writes through. events.MoveLedger and events.Replay
// open their own system transaction, and the request already holds a tenant one —
// a system transaction cannot widen it, so the call is made detached, exactly as
// the operator's tenant routes make theirs (modules/tenant/internal/handler.go). The
// cost of detaching is the cost stated there: the write stands whether or not the
// response reaches this caller.
func opsConn(ctx context.Context) (*db.Conn, error) {
	conn, ok := httpx.ConnFrom(ctx)
	if !ok {
		return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	return conn, nil
}

// ledgerFault says the one refusal a move can meet from a live deployment: a claim
// is open under a durable this move is about to rename. It is correctable by asking
// again, and the answer says so — nothing moved, nothing was emitted, and the boot's
// own retry and the ledger-move job are already doing this work.
func ledgerFault(err error) error {
	if errors.Is(err, events.ErrLedgerMoveContended) {
		return problem.New(http.StatusConflict,
			"events: a delivery is mid-claim on a ledger this move would rename; nothing was moved and nothing was recorded, and asking again is the remedy")
	}
	// Anything else travels as it came. kit/rest's own fault mapper is out of
	// reach here — kit/app sits below it (scripts/check_packages.sh says so by
	// name) — and a kernel command's error sentence is already the honest one: it
	// names the ledger and says whether rows moved, and kit/httpx answers an error
	// it cannot classify as the 500 that says the request that made it may be
	// repeated once the fault behind it is gone.
	return err
}

// replayFault says the one refusal a replay can meet that the caller could not have
// known: the outbox row is gone, and with it the last copy of the payload.
func replayFault(err error) error {
	if errors.Is(err, events.ErrNothingToReplay) {
		return problem.New(http.StatusNotFound,
			"events: no outbox row to replay; the row holds the payload's last copy, so it cannot be rebuilt from here")
	}
	// As above: events.Replay's own refusals — the missing reason, the unnamed
	// actor — are sentences a caller can act on, and they arrive as the problem
	// kit/httpx makes of an error carrying no status.
	return err
}

// mountLedgerOps mounts the two verbs. It is called from buildAPI for a
// composition that names itself, beside the one JSON door the kernel already owns.
func mountLedgerOps(api *httpx.API, own appname.Name) {
	kernel := api.Surfaces("")
	httpx.Register(kernel.Ops, huma.Operation{
		OperationID:   "app-ledger-move",
		Method:        http.MethodPost,
		Path:          "/ledger/move",
		Summary:       "Move this app's delivery ledgers onto its own durable",
		Description:   "Renames the handled and dead-letter rows of every tenant this app holds from the unscoped durable to the app-scoped one, in one transaction, and records one platformkit.ledger_moved row per tenant. It moves nothing for a tenant of another app. A delivery whose claim is open right now refuses the whole move and writes nothing; the boot and the ledger-move job run the same step, so an operator asks for it early, not for it to happen at all.",
		DefaultStatus: http.StatusOK,
		Tags:          []string{"kernel"},
		Errors:        []int{http.StatusUnprocessableEntity, http.StatusConflict, http.StatusServiceUnavailable},
		Extensions:    map[string]any{httpx.EventsExtension: []string{events.EventLedgerMoved}},
	}, httpx.OperatorPermission(PermissionLedgerMove), func(ctx context.Context, in *ledgerMoveInput) (*ledgerMoveReport, error) {
		if _, err := namedApp(own, in.Body.App); err != nil {
			return nil, err
		}
		conn, err := opsConn(ctx)
		if err != nil {
			return nil, err
		}
		report, err := events.MoveLedger(db.Detached(ctx), conn, own, "ops")
		if err != nil {
			return nil, ledgerFault(err)
		}
		return &ledgerMoveReport{
			Subscriptions: report.Subscriptions, Claims: report.Claims,
			Dead: report.Dead, Tenants: report.Tenants,
		}, nil
	})

	httpx.Register(kernel.Ops, huma.Operation{
		OperationID:   "app-event-replay",
		Method:        http.MethodPost,
		Path:          "/events/{id}/replay",
		Summary:       "Deliver one event again",
		Description:   "Clears the handling claims and terminal failures of one outbox row and returns the row to pending, so the relay carries it again; the claims clear for one durable when the body names one, and for every subscription that claimed the event when it names none. It is the verb behind a dead letter that has been fixed rather than forgiven: the event's payload stays in its outbox row, which is the last copy of it, so an id with no row there is refused and cannot be replayed from elsewhere. Records platformkit.event_replayed in the tenant whose event it was.",
		DefaultStatus: http.StatusOK,
		Tags:          []string{"kernel"},
		Errors:        []int{http.StatusUnprocessableEntity, http.StatusNotFound, http.StatusServiceUnavailable},
		Extensions:    map[string]any{httpx.EventsExtension: []string{events.EventReplayed}},
	}, httpx.OperatorPermission(PermissionEventReplay), func(ctx context.Context, in *eventReplayInput) (*eventReplayReport, error) {
		if _, err := namedApp(own, in.Body.App); err != nil {
			return nil, err
		}
		conn, err := opsConn(ctx)
		if err != nil {
			return nil, err
		}
		rec, err := events.Replay(db.Detached(ctx), conn, in.ID, in.Body.Durable, in.Body.Reason)
		if err != nil {
			return nil, replayFault(err)
		}
		return &eventReplayReport{EventID: rec.EventID, Name: rec.Name, Durable: rec.Durable, Reason: rec.Reason}, nil
	})
}
