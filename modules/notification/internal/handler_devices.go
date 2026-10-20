package internal

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// devicePath is the collection under the module's own path. It is a sub-resource
// of /notifications rather than a sibling because it is the same question the
// notifications answer — where does this person get told — and a client that has
// the list of notices already has the credential that reads it.
const devicePath = path + "/devices"

// RegisterDeviceRoutes mounts the three routes a person has over their own
// devices, next to the two they have over their own notices, on the same face and
// for the same reason: there is no rest.Spec here, because a Spec's list route is
// the whole tenant and these rows are somebody's. Nothing in this file takes a
// recipient as a parameter — the caller is the person, and the command rechecks
// that inside the transaction it writes in.
func RegisterDeviceRoutes(r *httpx.Router, devices contracts.DeviceService) {
	httpx.Register(r, huma.Operation{
		OperationID: "notification-device-list",
		Method:      http.MethodGet,
		Path:        devicePath,
		Summary:     "List my registered devices",
		Description: "The caller's own devices, newest first, with the tokens registered for them. There is no route that lists anybody else's.",
		Tags:        []string{"notification"},
		Errors:      faults,
	}, httpx.SignedIn(), func(ctx context.Context, in *listInput) (*rest.Page[*contracts.Device], error) {
		tx, me, err := caller(ctx)
		if err != nil {
			return nil, err
		}
		items, total, err := devices.ListDevices(ctx, tx, me, crud.Query{Limit: in.Limit, Offset: in.Offset, Sort: in.Sort})
		if err != nil {
			return nil, rest.Fault(err)
		}
		out := &rest.Page[*contracts.Device]{}
		out.Body.Items, out.Body.Total = items, total
		out.Body.Limit, out.Body.Offset = in.Limit, in.Offset
		return out, nil
	})

	httpx.Register(r, huma.Operation{
		OperationID: "notification-device-register",
		Method:      http.MethodPost,
		Path:        devicePath,
		Summary:     "Register a device of mine",
		Description: "The token the person's own client holds, for a platform this installation carries. Registering the one that is already registered changes nothing and publishes nothing; it becomes the caller's if somebody else held it.",
		Tags:        []string{"notification"},
		Errors:      faults,
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventDeviceRegistered}},
	}, httpx.SignedIn(), func(ctx context.Context, in *registerDeviceInput) (*rest.Item[*contracts.Device], error) {
		tx, me, err := caller(ctx)
		if err != nil {
			return nil, err
		}
		row, err := devices.RegisterDevice(ctx, tx, me, contracts.DeviceInput{Platform: in.Body.Platform, Token: in.Body.Token})
		return &rest.Item[*contracts.Device]{Body: row}, rest.Fault(err)
	})

	httpx.Register(r, huma.Operation{
		OperationID: "notification-device-remove",
		Method:      http.MethodPost,
		Path:        devicePath + "/{id}/remove",
		Summary:     "Remove one of my devices",
		Description: "A device id that is not one of the caller's live devices is not found: the answer to somebody else's is the answer to one that never existed.",
		Tags:        []string{"notification"},
		Errors:      faults,
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventDeviceRemoved}},
	}, httpx.SignedIn(), func(ctx context.Context, in *deviceIDInput) (*devicesDoneOutput, error) {
		tx, me, err := caller(ctx)
		if err != nil {
			return nil, err
		}
		if err := devices.RemoveDevice(ctx, tx, in.ID, me); err != nil {
			return nil, rest.Fault(err)
		}
		out := &devicesDoneOutput{}
		out.Body.Done = true
		return out, nil
	})
}

// registerDeviceInput is the body a person registers a device with. It names a
// platform and a token and nothing else: there is no field that says whose
// device it is, because the answer is whoever is asking.
type registerDeviceInput struct {
	Body struct {
		Platform push.Platform `json:"platform" maxLength:"16" doc:"What speaks this token" example:"expo"`
		Token    string        `json:"token" maxLength:"8192" doc:"The provider's token, or the browser's serialised subscription"`
	} `required:"true"`
}

type deviceIDInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The device to remove, as the list showed it"`
}

// devicesDoneOutput is the answer to a removal: it happened, and the device is
// not in the list any more, which is the read that shows it.
type devicesDoneOutput struct {
	Body struct {
		Done bool `json:"done"`
	}
}
