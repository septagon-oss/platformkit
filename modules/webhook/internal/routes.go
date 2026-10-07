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
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// The addresses the hand-mounted routes answer at, relative to the surface the
// kernel put them on. The Spec owns the two collections; these are the five
// routes whose answer, or whose checks, are not the entity.
const (
	createPath  = "/endpoints"
	rotatePath  = "/endpoints/{id}/rotate"
	replayPath  = "/deliveries/{id}/replay"
	targetVerb  = "target"
	enableVerb  = "enable"
	disableVerb = "disable"
)

// faults is what these answer with; 409 because enable, disable and a replay of
// a disabled endpoint are all conflicts with the row's state.
var faults = []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusServiceUnavailable}

// unavailable is the answer when a request carries no transaction, which the
// middleware has already logged.
var unavailable = problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")

// RegisterRoutes mounts the routes a Spec cannot: the two that answer with a
// secret, the one that answers with a different entity, and the three commands
// that move the lifecycle.
//
// The three commands are rest.Command, so they get the Spec's write guard, the
// catalogue's door record and the generated screen's button from the same place
// the CRUD routes do. The other three are hand-mounted because a Spec answers
// with the entity, and the answer here is a value the entity must never hold
// (auth's shown-once token is a separate type for the same reason) or is a
// different row entirely.
func RegisterRoutes(s httpx.Surfaces, spec rest.Spec[*contracts.Endpoint], svc *Service) {
	read := httpx.Permission(contracts.PermissionWebhookRead)
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

	// The replay: the answer is the queued attempt, which is a delivery row and
	// not the row named in the path — which is also why the delivery log's Spec
	// offers no write of its own.
	httpx.Register(s.App, huma.Operation{
		OperationID: "webhook-delivery-replay",
		Method:      http.MethodPost,
		Path:        replayPath,
		Summary:     "Send one event to one endpoint again",
		Description: "Queues a new attempt, marked as a replay, for the event and endpoint of the delivery named. " +
			"An endpoint that is disabled is refused with 409 and nothing is written; a pair with an attempt already " +
			"waiting answers with that attempt and writes nothing.",
		Tags:   []string{"webhook"},
		Errors: faults,
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventEndpointDisabled,
		}},
	}, write, func(ctx context.Context, in *idInput) (*rest.Item[*contracts.Delivery], error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, unavailable
		}
		row, err := svc.Replay(ctx, tx, in.ID)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return &rest.Item[*contracts.Delivery]{Body: row}, nil
	})

	// The three commands that answer with the endpoint.
	rest.Command(s, spec, targetVerb,
		"Change what an endpoint receives and where",
		"Replaces the address and the event list. Both are re-checked: the address against this deployment's policy, and every name against what this application emits.",
		[]string{contracts.EventEndpointUpdated},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in targetBody) (*contracts.Endpoint, error) {
			return svc.Target(ctx, tx, id, in.URL, contracts.EventNames(in.Events))
		}, rest.CommandOptions{Auth: write})

	rest.Command(s, spec, enableVerb,
		"Enable a webhook endpoint",
		"Takes a disabled endpoint back and starts the fault clock again. Enabling an enabled endpoint changes nothing.",
		[]string{contracts.EventEndpointEnabled},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.Endpoint, error) {
			return svc.Enable(ctx, tx, id)
		}, rest.CommandOptions{Auth: write})

	rest.Command(s, spec, disableVerb,
		"Disable a webhook endpoint",
		"Stops delivery now and ends this endpoint's queued attempts with a reason. Its history stays readable, and re-enabling delivers later events only.",
		[]string{contracts.EventEndpointDisabled},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.Endpoint, error) {
			return svc.Disable(ctx, tx, id)
		}, rest.CommandOptions{Auth: write})

	// The read door is named here so it is not a comment: the two Specs mount
	// every read this module has, and neither is reachable without it.
	_ = read
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
