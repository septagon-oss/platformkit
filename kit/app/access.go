package app

// access.go is the kernel's side of asking for access: the event the ask leaves
// in the trail, and the one JSON door. The command itself is kit/httpx's
// (kit/httpx/access.go), because the ask names a refusal this request just got;
// the browser's door is the composition's, mounted through Options.AccessPage,
// because the body of a page is ui's and kit may not import ui — the same
// division that makes the catalog route's body a function.

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// EventAccessRequested is the ask. The brief spelled it `access.requested`, and
// the name is renamed here rather than silently: module.Validate refuses a
// manifest that emits outside its own namespace, so a name in the `access.`
// namespace would be either a module named `access` — a fifth manifest answering
// questions kit/httpx and modules/user already own — or a kernel event lying
// about who emitted it. `security.` is the kernel's namespace for a refusal and
// its consequences, and security.denied already sits in it.
const EventAccessRequested = "security.access_requested"

// AccessRequested is EventAccessRequested's payload: who asked, for which grant,
// at which address, and how many people were told. Notified is in the open
// because "sent" is only ever true of the notices that were written: an ask in a
// tenant with nobody holding role management is a fact an operator can read out
// of the trail (0) rather than a page that claimed a delivery it did not make.
//
// The actor is the event's own — the outbox takes it from the request's
// principal — so the asker is not repeated here; RefusedPath is what the person
// was looking at when the guard answered, which is the address the refusal page
// must not offer them back.
type AccessRequested struct {
	Permission  string    `json:"permission"`
	Label       string    `json:"label"`
	Method      string    `json:"method,omitempty"`
	RefusedPath string    `json:"path,omitempty"`
	RequestID   string    `json:"requestId,omitempty"`
	UserID      uuid.UUID `json:"userId"`
	Notified    int       `json:"notified"`
}

// recordAccessRequest is httpx.Options.Accessed for this composition: the event,
// in the request's own transaction, beside the notices the command wrote — so an
// ask nobody was told about is also an ask that is not in the trail, and neither
// happens on its own.
func recordAccessRequest(ctx context.Context, r httpx.AccessRecord) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		// No transaction means the notices could not have been written either; the
		// command already refused the ask for that reason.
		return
	}
	// The failure is returned to the caller as the write's own failure by the
	// command's caller below: Publish errors on the transaction, and an ask whose
	// event did not commit is an ask that rolls back with it.
	_ = events.Publish(ctx, tx, EventAccessRequested, AccessRequested{
		Permission: r.Permission, Label: r.Label, Method: r.Method, RefusedPath: r.Path,
		RequestID: r.RequestID, UserID: r.UserID, Notified: r.Notified,
	})
}

// accessBody is the JSON door's body. A permission and the address it was
// refused at: there is no field here for a role, a person or a target, because
// the only thing an ask may choose is who was told.
type accessBody struct {
	Permission string `json:"permission" required:"true" maxLength:"64" doc:"The grant that was refused, as the catalogue names it" example:"task:read"`
	Path       string `json:"path,omitempty" maxLength:"512" doc:"The address the caller was refused at, as a path within this application" example:"/app/task/tasks"`
}

type accessInput struct {
	Body accessBody `required:"true"`
}

// accessAnswer is what was sent, and not a row: there is no row (the ask is an
// event and some notices), and a command with nothing to return must invent
// nothing to return.
type accessAnswer struct {
	Sent       bool   `json:"sent"`
	Permission string `json:"permission"`
	Label      string `json:"label"`
}

// mountAccessRequest mounts the kernel's ask door beside the catalog route. The
// address is the kernel's because no module owns "the sequel to a kernel
// refusal"; the guard is httpx.SignedIn(), because a permission every refused
// person would have to hold in order to ask for a permission decides nothing —
// the person who needs this door is, by definition, the one a grant is missing
// from. The principal comes from the session and the tenant from the
// transaction, never from the body.
func mountAccessRequest(api *httpx.API) {
	kernel := api.Surfaces("")
	httpx.Register(kernel.App, huma.Operation{
		OperationID:   "app-access-request",
		Method:        http.MethodPost,
		Path:          "/access-requests",
		Summary:       "Ask for a grant this tenant refused",
		Description:   "Records the ask and notifies the people in this tenant whose role grants role management. It grants nothing: the next request is refused exactly as this one was until somebody who may grant the role does so.",
		DefaultStatus: http.StatusAccepted,
		Tags:          []string{"kernel"},
		Errors:        []int{http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusServiceUnavailable},
		Extensions:    map[string]any{httpx.EventsExtension: []string{EventAccessRequested}},
	}, httpx.SignedIn(), func(ctx context.Context, in *accessInput) (*accessAnswer, error) {
		if err := httpx.Ask(ctx, httpx.AccessAsk{Permission: in.Body.Permission, Path: in.Body.Path}); err != nil {
			return nil, err
		}
		label := ""
		for _, g := range api.Permissions() {
			if g.Permission == in.Body.Permission {
				label = g.Label
			}
		}
		return &accessAnswer{Sent: true, Permission: in.Body.Permission, Label: label}, nil
	})
}
