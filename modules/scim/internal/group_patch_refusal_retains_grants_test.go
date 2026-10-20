package internal_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestRefusedGroupPatchPreservesMembershipRolesAndEventIdentities(t *testing.T) {
	w := compose(t, "finance")
	r := provisioningRouter(t, w)
	created := provisioningRequest(r, "POST", "/scim/v2/Users", `{"userName":"unchanged@acme.test","active":true}`)
	var person contracts.UserResource
	if err := json.Unmarshal(created.Body.Bytes(), &person); err != nil || created.Code != 201 {
		t.Fatalf("create person: %v %d %s", err, created.Code, created.Body)
	}
	created = provisioningRequest(r, "POST", "/scim/v2/Groups", `{"displayName":"Finance","externalId":"directory-finance"}`)
	var group contracts.GroupResource
	if err := json.Unmarshal(created.Body.Bytes(), &group); err != nil || created.Code != 201 {
		t.Fatalf("create group: %v %d %s", err, created.Code, created.Body)
	}
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.MapGroup(mayMap(ctx), tx, "Finance", "finance")
		return err
	})
	path := "/scim/v2/Groups/" + group.ID.String()
	before := provisioningRequest(r, "GET", path, "")
	if before.Code != 200 {
		t.Fatalf("read group: %d %s", before.Code, before.Body)
	}
	snapshot := func() ([]string, []string) {
		var roles, ids []string
		w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			u, err := w.users.Get(ctx, tx, person.ID)
			if err != nil {
				return err
			}
			roles = slices.Clone(u.Roles)
			return tx.DB().Table("platformkit_outbox").Order("id").Pluck("id", &ids).Error
		})
		return roles, ids
	}
	beforeRoles, beforeEvents := snapshot()
	if len(beforeRoles) != 0 || len(beforeEvents) == 0 {
		t.Fatalf("unexpected starting roles/events: %v %v", beforeRoles, beforeEvents)
	}
	add := `{"op":"add","path":"members","value":[{"value":"` + person.ID.String() + `"}]}`
	refused := provisioningRequest(r, "PATCH", path, `{"schemas":["`+contracts.SchemaPatchOp+`"],"Operations":[`+add+`,{"op":"replace","path":"unknownAttribute","value":"invalid"}]}`)
	var failure struct {
		Detail string `json:"detail"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(refused.Body.Bytes(), &failure); err != nil || refused.Code < 400 || failure.Detail == "" || failure.ID != "" {
		t.Fatalf("expected refusal with reason and no resource: %v %d %s", err, refused.Code, refused.Body)
	}
	after := provisioningRequest(r, "GET", path, "")
	if after.Code != 200 || before.Body.String() != after.Body.String() {
		t.Errorf("refused PATCH changed group identity or membership: before=%s after=%d %s", before.Body, after.Code, after.Body)
	}
	afterRoles, afterEvents := snapshot()
	if !slices.Equal(beforeRoles, afterRoles) || !slices.Equal(beforeEvents, afterEvents) {
		t.Errorf("refused PATCH changed roles/events: %v/%v -> %v/%v", beforeRoles, beforeEvents, afterRoles, afterEvents)
	}
	accepted := provisioningRequest(r, "PATCH", path, `{"schemas":["`+contracts.SchemaPatchOp+`"],"Operations":[`+add+`]}`)
	if err := json.Unmarshal(accepted.Body.Bytes(), &group); err != nil || accepted.Code != 200 {
		t.Fatalf("valid membership change: %v %d %s", err, accepted.Code, accepted.Body)
	}
	afterRoles, afterEvents = snapshot()
	if len(group.Members) != 1 || group.Members[0].Value != person.ID.String() || !slices.Contains(afterRoles, "finance") || len(afterEvents) <= len(beforeEvents) {
		t.Errorf("valid PATCH did not commit membership, role and events: %+v %v %v", group, afterRoles, afterEvents)
	}
}
