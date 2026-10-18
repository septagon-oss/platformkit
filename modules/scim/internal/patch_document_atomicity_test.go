package internal_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestProtocolPatchAppliesOperationsInOrderOrCommitsNothing(t *testing.T) {
	for _, collection := range []string{"Users", "Groups"} {
		t.Run(collection, func(t *testing.T) {
			w := compose(t)
			r := provisioningRouter(t, w)
			draft := `{"displayName":"Original","externalId":"directory-id"}`
			if collection == "Users" {
				draft = `{"userName":"patch@acme.test","displayName":"Original","externalId":"directory-id","active":true}`
			}
			created := provisioningRequest(r, "POST", "/scim/v2/"+collection, draft)
			if created.Code != 201 {
				t.Fatalf("create: %d %s", created.Code, created.Body)
			}
			var identity struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(created.Body.Bytes(), &identity); err != nil || identity.ID == "" {
				t.Fatalf("created identity: %v %s", err, created.Body)
			}
			path := "/scim/v2/" + collection + "/" + identity.ID
			first := `{"op":"replace","path":"externalId","value":"uncommitted"}`
			ordered := `{"op":"replace","path":"externalId","value":"first"},{"op":"replace","path":"externalId","value":"last"}`
			if collection == "Groups" {
				member := provisioningRequest(r, "POST", "/scim/v2/Users", `{"userName":"member@acme.test","active":true}`)
				if member.Code != 201 {
					t.Fatalf("create member: %d %s", member.Code, member.Body)
				}
				var person contracts.UserResource
				if err := json.Unmarshal(member.Body.Bytes(), &person); err != nil {
					t.Fatal(err)
				}
				first = `{"op":"add","path":"members","value":[{"value":"` + person.ID.String() + `"}]}`
				ordered = first + `,{"op":"remove","path":"members","value":[{"value":"` + person.ID.String() + `"}]}`
			}
			var before int
			w.inTenant(t, acme, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				before = len(outbox(t, tx))
				return nil
			})
			refused := provisioningRequest(r, "PATCH", path,
				`{"schemas":["`+contracts.SchemaPatchOp+`"],"Operations":[`+first+`,{"op":"replace","path":"unknownAttribute","value":"invalid"}]}`)
			if refused.Code < 400 {
				t.Fatalf("unsupported operation was accepted: %d %s", refused.Code, refused.Body)
			}
			var failure map[string]any
			if err := json.Unmarshal(refused.Body.Bytes(), &failure); err != nil {
				t.Fatalf("refusal document: %v", err)
			}
			if failure["detail"] == nil || failure["id"] != nil {
				t.Errorf("refusal must name a reason and return no resource: %s", refused.Body)
			}
			unchanged := provisioningRequest(r, "GET", path, "")
			var stored struct {
				DisplayName string                `json:"displayName"`
				ExternalID  string                `json:"externalId"`
				Members     []contracts.MemberRef `json:"members"`
			}
			if err := json.Unmarshal(unchanged.Body.Bytes(), &stored); err != nil || unchanged.Code != 200 {
				t.Fatalf("read after refusal: %v %d %s", err, unchanged.Code, unchanged.Body)
			}
			if stored.DisplayName != "Original" || stored.ExternalID != "directory-id" || len(stored.Members) != 0 {
				t.Errorf("partial PATCH changed the resource: %+v", stored)
			}
			w.inTenant(t, acme, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				if after := len(outbox(t, tx)); after != before {
					t.Errorf("refused PATCH changed the outbox: %d -> %d", before, after)
				}
				return nil
			})
			accepted := provisioningRequest(r, "PATCH", path,
				`{"schemas":["`+contracts.SchemaPatchOp+`"],"Operations":[`+ordered+`]}`)
			if err := json.Unmarshal(accepted.Body.Bytes(), &stored); err != nil || accepted.Code != 200 {
				t.Fatalf("valid PATCH: %v %d %s", err, accepted.Code, accepted.Body)
			}
			if collection == "Users" && stored.ExternalID != "last" || collection == "Groups" && len(stored.Members) != 0 {
				t.Errorf("PATCH operations were not applied in order: %+v", stored)
			}
		})
	}
}
