package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestCreatingAGroupLeavesATransactionalAuditEvent(t *testing.T) {
	w := compose(t)
	r := provisioningRouter(t, w)
	res := provisioningRequest(r, "POST", "/scim/v2/Groups", `{"displayName":"People"}`)
	if res.Code != 201 {
		t.Fatalf("create group: %d %s", res.Code, res.Body)
	}
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var rows []struct {
			Name    string
			Payload string
			Actor   *string
		}
		if err := tx.DB().Table("platformkit_outbox").Select("name, payload, actor").Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			t.Error("successful SCIM group write committed without an audit event")
		}
		for _, row := range rows {
			if row.Actor == nil || row.Payload == "" || row.Payload == "{}" {
				t.Errorf("group audit event has no actor or payload: %+v", row)
			}
		}
		return nil
	})
}
