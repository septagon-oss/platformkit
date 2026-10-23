package authtest

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func grantRolePermissions(t *testing.T, f Fixture) {
	f.Role("editor", "task:read", "task:update")
	f.User("ada@acme.example.com", Password, "editor")
	held, err := f.Service.Permissions(f.Ctx, f.Tx, []string{"editor"})
	if err != nil {
		t.Fatalf("Permissions: %v", err)
	}
	if !contracts.Grants(held, tenancy.Grant{Permission: "task:read"}) ||
		contracts.Grants(held, tenancy.Grant{Permission: "tenant:manage"}) {
		t.Errorf("editor holds %v; it grants what it names and nothing else", held)
	}
	// And a role nobody defined grants nothing rather than failing: a
	// user carrying a deleted role has less authority, not a broken
	// request.
	held, err = f.Service.Permissions(f.Ctx, f.Tx, []string{"ghost"})
	if err != nil || len(held) != 0 {
		t.Errorf("an undefined role = %v, %v; want nothing and no error", held, err)
	}
}

func refuseUndeclaredPermission(t *testing.T, f Fixture) {
	role, err := f.Service.SetRole(f.Ctx, f.Tx, "editor", []string{"widget:read", "widget:manage"}, Declared)
	if err != nil {
		t.Fatalf("SetRole: %v", err)
	}
	if !slices.Equal([]string(role.Grants), []string{"widget:manage", "widget:read"}) {
		t.Errorf("editor grants %v, want the two it was given, sorted", role.Grants)
	}
	held, err := f.Service.Permissions(f.Ctx, f.Tx, []string{"editor"})
	if err != nil || !contracts.Grants(held, tenancy.Grant{Permission: "widget:read"}) {
		t.Errorf("editor holds %v (%v), want what SetRole wrote", held, err)
	}
	// A permission nothing defines is refused rather than written: a
	// role naming one is a grant that can never be exercised and reads,
	// to whoever wrote it, exactly like one that can.
	if _, err := f.Service.SetRole(f.Ctx, f.Tx, "editor", []string{"widget:read", "ghost:read"}, Declared); !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("SetRole with an undefined permission = %v, want ErrInvalid", err)
	}
	// And the refusal changed nothing.
	if roles, err := f.Service.Roles(f.Ctx, f.Tx); err != nil {
		t.Fatalf("Roles: %v", err)
	} else if i := slices.IndexFunc(roles, func(r *contracts.Role) bool { return r.Name == "editor" }); i < 0 ||
		!slices.Equal([]string(roles[i].Grants), []string{"widget:manage", "widget:read"}) {
		t.Errorf("the roles are %v after a refused write", roles)
	}
	published(t, f, contracts.EventRoleSet)
}

func normaliseRole(t *testing.T, f Fixture) {
	permissions := []string{" WIDGET:READ ", "widget:read", "", "widget:manage"}
	role, err := f.Service.SetRole(f.Ctx, f.Tx, " Editor ", permissions, Declared)
	if err != nil {
		t.Fatalf("SetRole: %v", err)
	}
	if role.Name != "editor" || !slices.Equal([]string(role.Grants), []string{"widget:manage", "widget:read"}) {
		t.Fatalf("normalized role = %s %v", role.Name, role.Grants)
	}
	if permissions[0] != " WIDGET:READ " {
		t.Fatal("SetRole mutated its input")
	}
	if _, err := f.Service.SetRole(f.Ctx, f.Tx, "editor", []string{"widget:read", "widget:manage"}, Declared); err != nil {
		t.Fatalf("repeat SetRole: %v", err)
	}
	if _, err := f.Service.SetRole(f.Ctx, f.Tx, "bad-role", nil, Declared); !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("invalid role name = %v, want ErrInvalid", err)
	}
	published(t, f, contracts.EventRoleSet)
}

func reportRoleChange(t *testing.T, f Fixture) {
	// SetRoleChanged is SetRole plus the one answer SetRole computes for
	// itself and keeps: whether this call moved a row. The caller that
	// needs it is modules/auth's repair, whose command prints "removed" —
	// and prints it from an answer the write already had rather than from
	// its own subtraction, because only the write knows whether a row moved.
	if _, changed, err := f.Service.SetRoleChanged(f.Ctx, f.Tx, "editor",
		[]string{"widget:read"}, Declared); err != nil || !changed {
		t.Errorf("writing a new role: changed=%v (%v), want true", changed, err)
	}
	if _, changed, err := f.Service.SetRoleChanged(f.Ctx, f.Tx, "editor",
		[]string{"widget:read"}, Declared); err != nil || changed {
		t.Errorf("the same list again: changed=%v (%v), want false", changed, err)
	}
	if _, changed, err := f.Service.SetRoleChanged(f.Ctx, f.Tx, "editor",
		[]string{"widget:read", "widget:manage"}, Declared); err != nil || !changed {
		t.Errorf("a different list: changed=%v (%v), want true", changed, err)
	}
	// The refusals answer the same way they do for SetRole, with nothing
	// changed: this method adds an answer and takes no rule away.
	if _, changed, err := f.Service.SetRoleChanged(f.Ctx, f.Tx, "editor",
		[]string{"widget:read", "ghost:read"}, Declared); !errors.Is(err, crud.ErrInvalid) || changed {
		t.Errorf("a permission nothing defines: changed=%v (%v), want false and ErrInvalid", changed, err)
	}
}

func preserveLastRole(t *testing.T, f Fixture) {
	manage := tenancy.Grant{Permission: contracts.PermissionRoleManage}
	// The wildcard is the only way this catalogue can grant role:manage
	// — Declared is deliberately nobody's real permissions — and it
	// grants it the way it grants every ordinary permission.
	if _, err := f.Service.SetRole(f.Ctx, f.Tx, "owner", []string{contracts.Wildcard}, Declared); err != nil {
		t.Fatalf("SetRole owner: %v", err)
	}
	// Whatever else this fixture seeded, owner is now the only role that
	// can administer roles. Emptying the others is allowed, one at a
	// time, which is the rule read from the other side.
	roles, err := f.Service.Roles(f.Ctx, f.Tx)
	if err != nil {
		t.Fatalf("Roles: %v", err)
	}
	for _, role := range roles {
		if role.Name == "owner" || !contracts.Grants(role.Grants, manage) {
			continue
		}
		if _, err := f.Service.SetRole(f.Ctx, f.Tx, role.Name, nil, Declared); err != nil {
			t.Fatalf("emptying %s while owner can still administer = %v", role.Name, err)
		}
	}
	// Emptying the last one is what a person does by unticking a box
	// and pressing save. It is refused, because afterwards nobody in
	// this tenant could change a role again through any door.
	_, err = f.Service.SetRole(f.Ctx, f.Tx, "owner", nil, Declared)
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("emptying the last administering role = %v, want ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), contracts.PermissionRoleManage) {
		t.Errorf("the refusal does not name the permission: %v", err)
	}
	if held, err := f.Service.Permissions(f.Ctx, f.Tx, []string{"owner"}); err != nil || !contracts.Grants(held, manage) {
		t.Errorf("owner holds %v (%v) after a refused write, want what it had", held, err)
	}
	// A second role that can administer makes the first ordinary again.
	// This is the rule's boundary and not a statement about the
	// product: the rule must not be stricter, or a tenant with a
	// mistake in its roles could not be repaired. It is also the shape
	// of what the rule cannot see — a role nobody holds satisfies it
	// just as well. See contracts.CheckedAdministration.
	if _, err := f.Service.SetRole(f.Ctx, f.Tx, "second", []string{contracts.Wildcard}, Declared); err != nil {
		t.Fatalf("SetRole second: %v", err)
	}
	if _, err := f.Service.SetRole(f.Ctx, f.Tx, "owner", nil, Declared); err != nil {
		t.Errorf("emptying a role that is not the last administrator = %v", err)
	}
	// And a role that never granted it is never the last one: this is a
	// grant leaving, not a demand that one exist.
	if _, err := f.Service.SetRole(f.Ctx, f.Tx, "reader", []string{"widget:read"}, Declared); err != nil {
		t.Errorf("SetRole reader: %v", err)
	}
}

func boundRoleName(t *testing.T, f Fixture) {
	// The route's path parameter and the screen's control both declare
	// 64; without the rule underneath them, a caller that is neither
	// stores whatever it likes.
	long := strings.Repeat("a", contracts.MaxRoleName+1)
	_, err := f.Service.SetRole(f.Ctx, f.Tx, long, []string{"widget:read"}, Declared)
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("a %d-character role name = %v, want ErrInvalid", len(long), err)
	}
	if _, err := f.Service.SetRole(f.Ctx, f.Tx, long[:contracts.MaxRoleName], []string{"widget:read"}, Declared); err != nil {
		t.Errorf("a role name of exactly %d characters = %v", contracts.MaxRoleName, err)
	}
	published(t, f, contracts.EventRoleSet)
}

func refuseOperatorPermission(t *testing.T, f Fixture) {
	// This tenant is a customer's — every fixture's is — so naming the
	// installation's own permission in one of its roles is refused. It
	// would be a grant that looks like authority and is not: the kernel
	// refuses an operator route here before it reads any role at all.
	_, err := f.Service.SetRole(f.Ctx, f.Tx, "operator", []string{"fleet:manage"}, Declared)
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("SetRole with an operator permission = %v, want ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "fleet:manage") {
		t.Errorf("the refusal does not name the permission: %v", err)
	}
	published(t, f)
}
