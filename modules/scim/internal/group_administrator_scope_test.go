package internal_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestAGroupRemovalCountsOnlyActiveAdministratorsOfItsTenant(t *testing.T) {
	for _, other := range []string{"inactive", "foreign tenant"} {
		t.Run(other, func(t *testing.T) {
			w := compose(t, "admin")
			r := provisioningRouter(t, w)
			person := createdID(t, provisioningRequest(r, "POST", "/scim/v2/Users", `{"userName":"admin@acme.test","active":true}`))
			group := createdID(t, provisioningRequest(r, "POST", "/scim/v2/Groups", `{"displayName":"Admins","members":[{"value":"`+person.String()+`"}]}`))
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := w.svc.MapGroup(mayMap(ctx), tx, "Admins", "admin")
				return err
			})
			tenant := acme
			if other == "foreign tenant" {
				tenant = globex
			}
			w.inTenant(t, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{UserName: "other-admin@example.test", Active: new(true)})
				if err != nil {
					return err
				}
				if _, err := w.users.SetRoles(ctx, tx, u.ID, usercontracts.Roles{"admin"}); err != nil {
					return err
				}
				if other == "inactive" {
					return w.svc.DeactivateUser(ctx, tx, u.ID)
				}
				return nil
			})
			var before []string
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return tx.DB().Table("platformkit_outbox").Where("name <> ?", usercontracts.EventAdministrationRefused).Order("id").Pluck("id", &before).Error
			})
			path := "/scim/v2/Groups/" + group.String()
			body := `{"schemas":["` + contracts.SchemaPatchOp + `"],"Operations":[{"op":"remove","path":"members[value eq \"` + person.String() + `\"]"}]}`
			res := provisioningRequest(r, "PATCH", path, body)
			var failure struct{ Detail, ID string }
			if err := json.Unmarshal(res.Body.Bytes(), &failure); err != nil || res.Code < 400 || res.Code >= 500 || failure.Detail == "" || failure.ID != "" {
				t.Errorf("want refusal with a reason and no row: %v %d %s", err, res.Code, res.Body)
			}
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				u, err := w.users.Get(ctx, tx, person)
				if err != nil {
					return err
				}
				g, err := w.svc.GetGroup(ctx, tx, group)
				if err != nil {
					return err
				}
				var after []string
				if err := tx.DB().Table("platformkit_outbox").Where("name <> ?", usercontracts.EventAdministrationRefused).Order("id").Pluck("id", &after).Error; err != nil {
					return err
				}
				if !slices.Contains(u.Roles, "admin") || g.Role != "admin" || len(g.Members) != 1 || g.Members[0].Value != person.String() || !slices.Equal(before, after) {
					t.Errorf("refusal changed roles, mapping, membership or events: roles=%v group=%+v events=%v -> %v", u.Roles, g, before, after)
				}
				return nil
			})
			second := createdID(t, provisioningRequest(r, "POST", "/scim/v2/Users", `{"userName":"active-backup@acme.test","active":true}`))
			add := `{"schemas":["` + contracts.SchemaPatchOp + `"],"Operations":[{"op":"add","path":"members","value":[{"value":"` + second.String() + `"}]}]}`
			if res := provisioningRequest(r, "PATCH", path, add); res.Code != 200 {
				t.Fatalf("active administrator could not join: %d %s", res.Code, res.Body)
			}
			if res := provisioningRequest(r, "PATCH", path, body); res.Code != 200 {
				t.Errorf("removal with an active administrator remaining: %d %s", res.Code, res.Body)
			}
		})
	}
}
