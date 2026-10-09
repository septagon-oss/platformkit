package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

// RFC 7644 section 3.5.2.2 and the Entra provisioning guide use a valuePath
// without a value body to remove one member.
func TestFilteredMemberRemovalPreservesOtherMembers(t *testing.T) {
	w := compose(t)
	var group, departing, remaining uuid.UUID
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		a, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{UserName: "departing@acme.test"})
		if err != nil {
			return err
		}
		departing = a.ID
		b, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{UserName: "remaining@acme.test"})
		if err != nil {
			return err
		}
		remaining = b.ID
		g, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: "People", Members: []contracts.MemberRef{{Value: departing.String()}, {Value: remaining.String()}}})
		if err != nil {
			return err
		}
		group = g.ID
		return nil
	})
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.PatchGroup(ctx, tx, group, []contracts.Operation{{Op: "remove", Path: `members[value eq "` + departing.String() + `"]`}})
		return err
	})
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		g, err := w.svc.GetGroup(ctx, tx, group)
		if err != nil {
			return err
		}
		if len(g.Members) != 1 || g.Members[0].Value != remaining.String() {
			t.Errorf("removing one member left %v; want only %s", g.Members, remaining)
		}
		return nil
	})
}
