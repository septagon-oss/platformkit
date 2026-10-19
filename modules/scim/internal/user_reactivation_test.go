package internal_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestAConnectorCanReactivateThePersonItDeactivated(t *testing.T) {
	for _, tc := range []struct{ name, method, body string }{
		{"Entra", "PATCH", `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"Replace","path":"active","value":true}]}`},
		{"Okta", "PATCH", `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","value":{"active":true}}]}`},
		{"replace", "PUT", `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"returning@acme.test","active":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := compose(t)
			r := provisioningRouter(t, w)
			res := provisioningRequest(r, "POST", "/scim/v2/Users", `{"userName":"returning@acme.test","active":true}`)
			if res.Code != 201 {
				t.Fatalf("create: %d %s", res.Code, res.Body)
			}
			var person contracts.UserResource
			if err := json.Unmarshal(res.Body.Bytes(), &person); err != nil {
				t.Fatal(err)
			}
			path := "/scim/v2/Users/" + person.ID.String()
			res = provisioningRequest(r, "PATCH", path, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"active","value":false}]}`)
			if res.Code != 200 {
				t.Fatalf("deactivate: %d %s", res.Code, res.Body)
			}
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				u, err := w.svc.GetUser(ctx, tx, person.ID)
				if err == nil && u.Active {
					t.Fatal("deactivation did not reach inactive state")
				}
				return err
			})
			res = provisioningRequest(r, tc.method, path, tc.body)
			if res.Code != 200 && res.Code != 204 {
				t.Errorf("reactivation: %d %s; want successful update", res.Code, res.Body)
			}
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				u, err := w.svc.GetUser(ctx, tx, person.ID)
				if err == nil && !u.Active {
					t.Error("connector's returning person remains inactive")
				}
				return err
			})
		})
	}
}
