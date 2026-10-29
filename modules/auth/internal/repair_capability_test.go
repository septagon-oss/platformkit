package internal_test

import (
	"context"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
)

type repairOnly struct {
	reads, writes int
	kept          []string
}

func (r *repairOnly) Roles(context.Context, db.Tx[db.Tenant]) ([]*contracts.Role, error) {
	r.reads++
	held := []string{"old:read", "kept:read"}
	if r.reads > 1 {
		held = append(held, "new:read")
	}
	return []*contracts.Role{{Name: "member", Grants: held}}, nil
}
func (r *repairOnly) SetRoleChanged(_ context.Context, _ db.Tx[db.Tenant], _ string, permissions []string, _ []tenancy.Grant) (*contracts.Role, bool, error) {
	r.writes++
	r.kept = slices.Clone(permissions)
	return &contracts.Role{Name: "member", Grants: permissions}, true, nil
}

func TestRoleRepairUsesOnlyItsCapabilityAndRereadsBeforeWriting(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	for _, remove := range []bool{false, true} {
		reader := &repairOnly{}
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			found, err := auth.RepairSeededRolesWith(ctx, tx, reader,
				[]tenancy.Grant{{Permission: "kept:read"}, {Permission: "new:read"}},
				[]contracts.Role{{Name: "member", Grants: []string{"old:read", "kept:read"}}}, remove)
			if err == nil && !slices.Equal(found["member"], []string{"old:read"}) {
				t.Fatalf("repair report = %v", found)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if !remove && (reader.reads != 1 || reader.writes != 0) {
			t.Fatalf("dry run = %+v", reader)
		}
		if remove && (reader.reads != 2 || reader.writes != 1 || !slices.Equal(reader.kept, []string{"kept:read", "new:read"})) {
			t.Fatalf("locked repair = %+v", reader)
		}
	}
}
