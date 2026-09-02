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
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
)

// path is the subscription, which is one resource and not a collection: a
// tenant is the customer, so it has one.
const path = "/api/v1/billing/subscription"

var (
	faults      = []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusServiceUnavailable}
	unavailable = problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
)

// RegisterRoutes mounts the three routes a singleton has: read it, subscribe,
// cancel. There is no rest.Spec, and the reason is one sentence: a Spec is five
// routes on a collection, and there is one subscription per tenant. The plans
// beside it are a collection and do have one; ../module.go mounts it.
func RegisterRoutes(api *httpx.API, svc contracts.Service) {
	httpx.Register(api, huma.Operation{
		OperationID: "billing-subscription-read",
		Method:      http.MethodGet,
		Path:        path,
		Summary:     "Read this tenant's subscription",
		Description: "A tenant that has never subscribed has no subscription, which is a 404.",
		Tags:        []string{"billing"},
		Errors:      faults,
	}, httpx.Permission(contracts.PermissionBillingRead),
		func(ctx context.Context, _ *struct{}) (*rest.Item[*contracts.Subscription], error) {
			return answer(ctx, func(tx db.Tx[db.Tenant]) (*contracts.Subscription, error) {
				return svc.Current(ctx, tx)
			})
		})

	command(api, "subscribe", "Subscribe this tenant to a plan",
		"Subscribing to the plan already in force changes nothing. A different plan takes effect at the next renewal; the period being served is not moved.",
		contracts.EventSubscribed,
		func(ctx context.Context, tx db.Tx[db.Tenant], in subscribeBody) (*contracts.Subscription, error) {
			return svc.Subscribe(ctx, tx, in.PlanID)
		})

	command(api, "cancel", "Cancel this tenant's subscription",
		"At the end of the period being served, or now. Neither shortens the period: cancelling is not a refund.",
		contracts.EventCancelled,
		func(ctx context.Context, tx db.Tx[db.Tenant], in cancelBody) (*contracts.Subscription, error) {
			return svc.Cancel(ctx, tx, in.AtPeriodEnd)
		})
}

// command mounts one POST on the singleton, guarded by billing:manage and
// declaring the event it publishes. It is kit/rest's Command with the id taken
// out, which is the only thing a singleton changes about a command.
func command[I any](api *httpx.API, verb, summary, description, event string,
	run func(ctx context.Context, tx db.Tx[db.Tenant], in I) (*contracts.Subscription, error),
) {
	httpx.Register(api, huma.Operation{
		OperationID: "billing-subscription-" + verb,
		Method:      http.MethodPost,
		Path:        path + "/" + verb,
		Summary:     summary,
		Description: description,
		Tags:        []string{"billing"},
		Errors:      faults,
		Extensions:  map[string]any{httpx.EventsExtension: []string{event}},
	}, httpx.Permission(contracts.PermissionBillingManage),
		func(ctx context.Context, in *struct{ Body *I }) (*rest.Item[*contracts.Subscription], error) {
			var body I
			if in.Body != nil {
				body = *in.Body
			}
			return answer(ctx, func(tx db.Tx[db.Tenant]) (*contracts.Subscription, error) {
				return run(ctx, tx, body)
			})
		})
}

// answer runs one handler against the request's transaction and maps kit/crud's
// errors the way kit/rest does, so this module holds no second opinion about
// what a 404 means.
func answer(ctx context.Context, run func(db.Tx[db.Tenant]) (*contracts.Subscription, error)) (*rest.Item[*contracts.Subscription], error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return nil, unavailable
	}
	sub, err := run(tx)
	if err != nil {
		return nil, rest.Fault(err)
	}
	return &rest.Item[*contracts.Subscription]{Body: sub}, nil
}

type subscribeBody struct {
	PlanID uuid.UUID `json:"planId" format:"uuid" doc:"The plan to subscribe to"`
}

type cancelBody struct {
	// AtPeriodEnd defaults to false, so a caller that says nothing cancels now:
	// the silent default is the one that stops charging.
	AtPeriodEnd bool `json:"atPeriodEnd,omitempty" doc:"Keep serving until the current period ends" default:"false" required:"false"`
}
