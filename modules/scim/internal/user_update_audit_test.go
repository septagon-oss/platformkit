package internal_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestEveryUserAttributeWriteLeavesATransactionalAuditEvent(t *testing.T) {
	for _, attribute := range []string{"displayName", "externalId"} {
		t.Run(attribute, func(t *testing.T) {
			w := compose(t)
			r := provisioningRouter(t, w)
			created := provisioningRequest(r, "POST", "/scim/v2/Users", `{"userName":"person@acme.test","active":true}`)
			if created.Code != 201 {
				t.Fatalf("create: %d %s", created.Code, created.Body)
			}
			var person contracts.UserResource
			if err := json.Unmarshal(created.Body.Bytes(), &person); err != nil {
				t.Fatal(err)
			}
			var before int
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				before = len(outbox(t, tx))
				return nil
			})
			body := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"` + attribute + `","value":"changed"}]}`
			res := provisioningRequest(r, "PATCH", "/scim/v2/Users/"+person.ID.String(), body)
			if res.Code != 200 {
				t.Fatalf("patch: %d %s", res.Code, res.Body)
			}
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				got, err := w.svc.GetUser(ctx, tx, person.ID)
				if err != nil {
					return err
				}
				value := got.DisplayName
				if attribute == "externalId" {
					value = got.ExternalID
				}
				if value != "changed" {
					t.Errorf("committed %s = %q", attribute, value)
				}
				if after := len(outbox(t, tx)); after <= before {
					t.Errorf("successful %s write committed without an audit event: outbox %d -> %d", attribute, before, after)
				}
				return nil
			})
		})
	}
}
