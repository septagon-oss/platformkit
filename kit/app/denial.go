package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// EventDenied is the event every attributable refused authorization publishes. It is
// the kernel's rather than a module's, because the refusal is the kernel's: no module
// ran. modules/audit subscribes to every event, so a composition that composes the
// audit module keeps each one as an audit row with the actor the request carried. It is
// listed in module.KernelEvents, which is what subscribes a SubscribeAll module to it.
const EventDenied = "security.denied"

// Denied is EventDenied's payload: what was refused and why, and the request id that
// joins it to the response and the log line. The principal is the event's actor (the
// outbox takes it from the context), so it is not repeated here; no request body or
// header is, because an event is copied into a table administrators read.
type Denied struct {
	Status     int       `json:"status"`
	Code       string    `json:"code"`
	Detail     string    `json:"detail"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Permission string    `json:"permission,omitempty"`
	Label      string    `json:"label,omitempty"`
	Operation  string    `json:"operation,omitempty"`
	RequestID  string    `json:"requestId,omitempty"`
	UserID     uuid.UUID `json:"userId"`
}

// recordDenial is httpx.Options.Denied for this composition: one event, in a transaction
// of the refused tenant's own that commits whatever the refused request does. A failure to record is logged and does not change the
// answer, which the caller already has; it is an error, because an unaudited denial is
// the thing this hook exists to prevent.
func recordDenial(conn *db.Conn, log *slog.Logger) func(context.Context, httpx.Denial) {
	return func(ctx context.Context, d httpx.Denial) {
		// Detached: the refused request's own transaction is rolled back by the refusal, and a
		// db.Run nested in it would roll the record back with it — the first version of this
		// did exactly that. A denial is a write meant to be kept, the case db.Detached names.
		err := db.Run(tenancy.WithTenant(db.Detached(ctx), d.Tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, EventDenied, Denied{
				Status: d.Status, Code: d.Code, Detail: d.Detail, Method: d.Method, Path: d.Path,
				Permission: d.Permission, Label: d.Label,
				Operation: d.Operation, RequestID: d.RequestID, UserID: d.Principal.UserID,
			})
		})
		if err != nil {
			log.ErrorContext(ctx, "app: a denial could not be recorded", "code", d.Code, "path", d.Path,
				"request", d.RequestID, "tenant", d.Tenant.Slug, "error", err)
		}
	}
}
