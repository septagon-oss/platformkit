package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// TestAGroupWriteCannotTakeTheLastAdministratorsRoleAway checks that every way a
// group can take back the role its mapping granted — a member removed by value or
// by the valuePath Entra ID sends, the member list emptied, the group replaced
// without them, the mapping withdrawn or moved — is refused when that role is what
// keeps the tenant administrable, and leaves the role, the membership, the mapping
// and the outbox as they were, but for the user module's own record of the refused
// attempt, which it writes outside the request's transaction on purpose. Once a
// second person holds the role through the same group, the same removal is
// accepted: the refusal is about the last one only.
func TestAGroupWriteCannotTakeTheLastAdministratorsRoleAway(t *testing.T) {
	w := compose(t, "admin", "editor")
	r := provisioningRouter(t, w)
	person := createdID(t, provisioningRequest(r, "POST", "/scim/v2/Users", `{"userName":"only-admin@acme.test","active":true}`))
	group := createdID(t, provisioningRequest(r, "POST", "/scim/v2/Groups", `{"displayName":"Admins","members":[{"value":"`+person.String()+`"}]}`))
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.MapGroup(mayMap(ctx), tx, "Admins", "admin")
		return err
	})
	snapshot := func() administrationState {
		var s administrationState
		w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			u, err := w.users.Get(ctx, tx, person)
			if err != nil {
				return err
			}
			s.roles = slices.Clone(u.Roles)
			g, err := w.svc.GetGroup(ctx, tx, group)
			if err != nil {
				return err
			}
			for _, m := range g.Members {
				s.members = append(s.members, m.Value)
			}
			mappings, err := w.svc.GroupMappings(ctx, tx)
			if err != nil {
				return err
			}
			for _, m := range mappings {
				s.mapped = append(s.mapped, m.Group+"="+m.Role)
			}
			return tx.DB().Table("platformkit_outbox").Where("name <> ?", usercontracts.EventAdministrationRefused).
				Order("id").Pluck("id", &s.events).Error
		})
		return s
	}
	before := snapshot()
	if !slices.Contains(before.roles, "admin") || !slices.Contains(before.members, person.String()) {
		t.Fatalf("setup: the mapped group did not make its member the administrator: %+v", before)
	}
	path := "/scim/v2/Groups/" + group.String()
	patch := func(op string) string {
		return `{"schemas":["` + contracts.SchemaPatchOp + `"],"Operations":[` + op + `]}`
	}
	for _, tc := range []struct{ name, method, body string }{
		{"remove by value", "PATCH", patch(`{"op":"remove","path":"members","value":[{"value":"` + person.String() + `"}]}`)},
		{"remove by valuePath", "PATCH", patch(`{"op":"Remove","path":"members[value eq \"` + person.String() + `\"]"}`)},
		{"replace with no members", "PATCH", patch(`{"op":"replace","path":"members","value":[]}`)},
		{"put without members", "PUT", `{"schemas":["` + contracts.SchemaGroup + `"],"displayName":"Admins","members":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := provisioningRequest(r, tc.method, path, tc.body)
			var failure struct {
				Status string `json:"status"`
				Detail string `json:"detail"`
				ID     string `json:"id"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &failure); err != nil || res.Code < 400 || res.Code >= 500 || failure.Detail == "" || failure.ID != "" {
				t.Errorf("want a refusal with a reason and no resource: %v %d %s", err, res.Code, res.Body)
			}
			if after := snapshot(); !before.equal(after) {
				t.Errorf("a refused group write changed state:\nbefore %+v\nafter  %+v", before, after)
			}
		})
	}
	for _, role := range []string{"", "editor"} {
		t.Run("map to "+role, func(t *testing.T) {
			err := db.Run(tenancy.WithTenant(t.Context(), acme), w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				m, err := w.svc.MapGroup(mayMap(ctx), tx, "Admins", role)
				if m != nil {
					t.Errorf("a refused mapping returned %+v", m)
				}
				return err
			})
			if !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("moving the last administrator's mapping gave %v, want a refusal", err)
			}
			if after := snapshot(); !before.equal(after) {
				t.Errorf("a refused mapping write changed state:\nbefore %+v\nafter  %+v", before, after)
			}
		})
	}
	second := createdID(t, provisioningRequest(r, "POST", "/scim/v2/Users", `{"userName":"second-admin@acme.test","active":true}`))
	if res := provisioningRequest(r, "PATCH", path, patch(`{"op":"add","path":"members","value":[{"value":"`+second.String()+`"}]}`)); res.Code != 200 {
		t.Fatalf("a second administrator could not join: %d %s", res.Code, res.Body)
	}
	if res := provisioningRequest(r, "PATCH", path, patch(`{"op":"remove","path":"members","value":[{"value":"`+person.String()+`"}]}`)); res.Code != 200 {
		t.Errorf("removing one of two administrators was refused: %d %s", res.Code, res.Body)
	}
	if after := snapshot(); slices.Contains(after.roles, "admin") {
		t.Errorf("the person who left kept the group's role: %v", after.roles)
	}
}

// administrationState is what a refused group write must leave as it found it.
type administrationState struct{ roles, members, mapped, events []string }

func (s administrationState) equal(o administrationState) bool {
	return slices.Equal(s.roles, o.roles) && slices.Equal(s.members, o.members) &&
		slices.Equal(s.mapped, o.mapped) && slices.Equal(s.events, o.events)
}

// createdID is the id of the resource a 201 answered with; anything else ends the case.
func createdID(t *testing.T, res *httptest.ResponseRecorder) uuid.UUID {
	t.Helper()
	var created struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil || res.Code != 201 || created.ID == uuid.Nil {
		t.Fatalf("create: %v %d %s", err, res.Code, res.Body)
	}
	return created.ID
}
