package internal

import (
	"context"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// The addresses the two shown-once routes answer at, relative to the surface
// the kernel put them on, and the verbs their doors are catalogued under. The
// Spec owns the two collections and `rest.Command` owns the five lifecycle
// routes; these are the routes whose answer is not the entity. The registration
// is a command over the collection — the row is what it produces — answered at
// the collection's own POST, and the rotation is one over a single endpoint.
const (
	createPath   = "/endpoints"
	rotatePath   = "/endpoints/{id}/rotate"
	registerVerb = "register"
	rotateVerb   = "rotate"
	replayVerb   = "replay"
	targetVerb   = "target"
	enableVerb   = "enable"
	disableVerb  = "disable"
)

// faults is what these answer with; 409 because enable, disable and a replay of
// a disabled endpoint are all conflicts with the row's state.
var faults = []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusServiceUnavailable}

// unavailable is the answer when a request carries no transaction, which the
// middleware has already logged.
var unavailable = problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")

// RegisterRoutes mounts the routes a Spec cannot: the two that answer with a
// secret, the one that answers with a different row, and the three commands that
// move the lifecycle.
//
// The lifecycle commands and the replay are `rest.Command`, so they get the
// Spec's write guard, the catalogue's record of the door and the generated
// screen's button from the same place the CRUD routes do — one declaration, and
// the API document, the catalogue and the screen cannot disagree about it.
//
// The create and the rotate are hand-mounted, because a Spec answers with the
// entity and the answer here is a value the entity must never hold (auth's
// shown-once token is a separate type for the same reason). Hand-mounting had a
// cost the second review found: `Resources()` is filled in beside `rest.Command`'s
// own mount, so a route mounted by hand was in the API and nowhere a generated
// client looks — registration and rotation were doors a screen could not draw
// and a device could not find. Both now record their own door, at the end, and
// the comment there says why a derived screen must not perform either one.
func RegisterRoutes(s httpx.Surfaces, endpoints rest.Spec[*contracts.Endpoint],
	deliveries rest.Spec[*contracts.Delivery], svc *Service) {
	write := httpx.Permission(contracts.PermissionWebhookUpdate)

	// The create: the only place besides the rotate where the signing secret
	// exists in cleartext, and so a route of its own rather than the Spec's
	// generated create, which answers with the row.
	httpx.Register(s.App, huma.Operation{
		OperationID: "webhook-endpoint-create",
		Method:      http.MethodPost,
		Path:        createPath,
		Summary:     "Register a webhook endpoint",
		Description: "An HTTPS address that wants events, and the event types it wants. " +
			"Every event type must be one this application emits. The signing secret is answered once, here, and by nothing else: " +
			"a caller who loses it rotates. The address is refused if it is not https or is not one this deployment dials.",
		Tags:   []string{"webhook"},
		Errors: faults,
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventEndpointCreated,
		}},
	}, write, func(ctx context.Context, in *createInput) (*rest.Item[*contracts.Issued], error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, unavailable
		}
		endpoint := &contracts.Endpoint{Name: in.Body.Name, URL: in.Body.URL, Events: contracts.EventNames(in.Body.Events)}
		issued, err := svc.Create(ctx, tx, endpoint)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return &rest.Item[*contracts.Issued]{Body: issued}, nil
	})

	httpx.Register(s.App, huma.Operation{
		OperationID: "webhook-endpoint-rotate",
		Method:      http.MethodPost,
		Path:        rotatePath,
		Summary:     "Replace an endpoint's signing secret",
		Description: "Mints a new secret and answers with it once. The old one stops signing immediately and is kept " +
			"for a day so a delivery already in flight can still be explained to a receiver that has not picked up the new one.",
		Tags:   []string{"webhook"},
		Errors: faults,
	}, write, func(ctx context.Context, in *idInput) (*rest.Item[*contracts.Issued], error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, unavailable
		}
		issued, err := svc.Rotate(ctx, tx, in.ID)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return &rest.Item[*contracts.Issued]{Body: issued}, nil
	})

	// The replay is rest.Command rather than a hand mount: its answer is the
	// entity of the log's own Spec — a *new* delivery row, which is why the log
	// offers no write of its own and why a fact stays a fact — and one
	// declaration is what puts the door in the API document, the catalogue and
	// the generated screen together rather than in two places that can disagree.
	// No events: a replay queues a row and publishes nothing.
	rest.Command(s, deliveries, replayVerb,
		"Send one event to one endpoint again",
		"Queues a new attempt, marked as a replay, for the event and endpoint of the delivery named. "+
			"An endpoint that is disabled is refused with 409 and nothing is written; a pair with an attempt already "+
			"waiting answers with that attempt and writes nothing.",
		nil,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.Delivery, error) {
			return svc.Replay(ctx, tx, id)
		}, rest.CommandOptions{Auth: write})

	// The three commands that answer with the endpoint.
	rest.Command(s, endpoints, targetVerb,
		"Change what an endpoint receives and where",
		"Replaces the address and the event list. Both are re-checked: the address against this deployment's policy, and every name against what this application emits.",
		[]string{contracts.EventEndpointUpdated},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in targetBody) (*contracts.Endpoint, error) {
			return svc.Target(ctx, tx, id, in.URL, contracts.EventNames(in.Events))
		}, rest.CommandOptions{Auth: write})

	rest.Command(s, endpoints, enableVerb,
		"Enable a webhook endpoint",
		"Takes a disabled endpoint back and starts the fault clock again. Enabling an enabled endpoint changes nothing.",
		[]string{contracts.EventEndpointEnabled},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.Endpoint, error) {
			return svc.Enable(ctx, tx, id)
		}, rest.CommandOptions{Auth: write})

	rest.Command(s, endpoints, disableVerb,
		"Disable a webhook endpoint",
		"Stops delivery now and ends this endpoint's queued attempts with a reason. Its history stays readable, and re-enabling delivers later events only.",
		[]string{contracts.EventEndpointDisabled},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.Endpoint, error) {
			return svc.Disable(ctx, tx, id)
		}, rest.CommandOptions{Auth: write})

	// The two doors above are hand-mounted because their answer is a signing
	// secret, and they are recorded on their resource by hand for the same
	// reason: `rest.Command` records the door beside the route it mounts, and a
	// route a Spec cannot mount leaves nothing in `Resources()` for a generated
	// screen or a native shell to read.
	//
	// Neither carries a Run, and that is the design rather than a gap: Run is
	// what a derived screen performs, and what a screen does after performing is
	// redirect — it has nowhere to show a value that exists in one response and
	// in no read. A page that rotated a secret would lose it, and an endpoint
	// whose administrator cannot see its key is an endpoint nobody can wire. So
	// the door is named — verb, argument shape, guard and absolute address — for
	// the clients that read a response, and ui/screens draws no button behind a
	// command with no Run, which is what `httpx.Command.Run`'s own doc says a nil
	// Run is for. The replay, whose answer is a row the log already lists, is a
	// button, and the kernel mounted it above.
	s.AddCommand(endpoints.Module, endpoints.Entity, httpx.Command{
		Verb: registerVerb, Collection: true,
		Summary: "Register a webhook endpoint",
		Description: "An HTTPS address that wants events, and the event types it wants; the signing secret is answered once, in this response. " +
			"Performed over the JSON route and by no derived screen, because the answer is a secret shown once.",
		Auth: write, Endpoint: s.App.Path(createPath),
		Fields: crud.FieldsOf(reflect.TypeFor[createBody]()),
	})
	s.AddCommand(endpoints.Module, endpoints.Entity, httpx.Command{
		Verb:    rotateVerb,
		Summary: "Replace an endpoint's signing secret",
		Description: "Mints a new secret and answers with it once; the old one stops signing immediately. " +
			"Performed over the JSON route and by no derived screen, because the answer is a secret shown once.",
		Auth: write, Endpoint: s.App.Path(rotatePath),
	})
}

// createInput is the new endpoint. The secret is not a field: the server mints
// it and answers with it, and a caller who could send one would be a caller who
// could choose a key for somebody else's receiver.
type createInput struct {
	Body *createBody
}

type createBody struct {
	Name   string   `json:"name" minLength:"1" maxLength:"120" doc:"What this endpoint is called" example:"Ops paging service"`
	URL    string   `json:"url" doc:"The HTTPS address" example:"https://hooks.example.com/platformkit"`
	Events []string `json:"events" minItems:"1" doc:"The event types to deliver; each must be one this application emits"`
}

// targetBody is the new destination. Both fields are required: a partial change
// would make the command's answer depend on which fields a caller forgot.
type targetBody struct {
	URL    string   `json:"url" doc:"The HTTPS address"`
	Events []string `json:"events" minItems:"1" doc:"The event types to deliver"`
}
