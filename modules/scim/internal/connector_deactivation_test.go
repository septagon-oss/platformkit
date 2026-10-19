package internal_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

// Shapes published in Microsoft's provisioning guide and Okta's SCIM 2.0 guide.
// A leaver must stop being active regardless of which connector sends the patch.
func TestConnectorDeactivationShapesStopThePerson(t *testing.T) {
	for _, tc := range []struct{ name, operations string }{
		{"Entra", `[{"op":"Replace","path":"active","value":false}]`},
		{"Okta", `[{"op":"replace","value":{"active":false}}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := compose(t)
			var id uuid.UUID
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{UserName: "leaver@acme.test", Active: new(true)})
				if err != nil {
					return err
				}
				id = u.ID
				return nil
			})
			r := provisioningRouter(t, w)
			res := provisioningRequest(r, "PATCH", "/scim/v2/Users/"+id.String(), fmt.Sprintf(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":%s}`, tc.operations))
			if res.Code != 200 && res.Code != 204 {
				t.Errorf("connector deactivation: %d %s", res.Code, res.Body)
			}
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				u, err := w.svc.GetUser(ctx, tx, id)
				if err != nil {
					return err
				}
				if u.Active {
					t.Error("deprovisioned person is still active")
				}
				return nil
			})
		})
	}
}
