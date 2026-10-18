package internal_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestRefusedUserPatchPreservesLifecycleAndEventIdentities(t *testing.T) {
	w := compose(t)
	r := provisioningRouter(t, w)
	created := provisioningRequest(r, "POST", "/scim/v2/Users", `{"userName":"retained@acme.test","externalId":"retained-id","active":true}`)
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	var person contracts.UserResource
	if err := json.Unmarshal(created.Body.Bytes(), &person); err != nil {
		t.Fatal(err)
	}
	path := "/scim/v2/Users/" + person.ID.String()
	before := provisioningRequest(r, "GET", path, "")
	if before.Code != 200 || !person.Active {
		t.Fatalf("active person unavailable: %d %s", before.Code, before.Body)
	}
	eventIDs := func() []string {
		var ids []string
		w.inTenant(t, acme, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Table("platformkit_outbox").Order("id").Pluck("id", &ids).Error
		})
		return ids
	}
	beforeEvents := eventIDs()
	if len(beforeEvents) == 0 {
		t.Fatal("create left no events to preserve")
	}
	refused := provisioningRequest(r, "PATCH", path, `{"schemas":["`+contracts.SchemaPatchOp+`"],"Operations":[{"op":"replace","path":"active","value":false},{"op":"replace","path":"unknownAttribute","value":"invalid"}]}`)
	var failure struct {
		Detail string `json:"detail"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(refused.Body.Bytes(), &failure); err != nil || refused.Code < 400 || failure.Detail == "" || failure.ID != "" {
		t.Fatalf("expected refusal with reason and no resource: %v %d %s", err, refused.Code, refused.Body)
	}
	after := provisioningRequest(r, "GET", path, "")
	if after.Code != 200 || before.Body.String() != after.Body.String() {
		t.Errorf("refused PATCH changed identity, lifecycle or metadata: before=%s after=%d %s", before.Body, after.Code, after.Body)
	}
	if afterEvents := eventIDs(); !slices.Equal(beforeEvents, afterEvents) {
		t.Errorf("refused PATCH changed event identities: %v -> %v", beforeEvents, afterEvents)
	}
	accepted := provisioningRequest(r, "PATCH", path, `{"schemas":["`+contracts.SchemaPatchOp+`"],"Operations":[{"op":"replace","path":"active","value":false}]}`)
	var stopped contracts.UserResource
	if err := json.Unmarshal(accepted.Body.Bytes(), &stopped); err != nil || accepted.Code != 200 {
		t.Fatalf("valid deactivation: %v %d %s", err, accepted.Code, accepted.Body)
	}
	if stopped.ID != person.ID || stopped.ExternalID != person.ExternalID || stopped.Active {
		t.Errorf("valid deactivation did not preserve the inactive identity: %+v", stopped)
	}
	if len(eventIDs()) <= len(beforeEvents) {
		t.Error("valid deactivation left no additional audit event")
	}
}
