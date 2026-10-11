package module

import (
	"strings"
	"testing"
)

// A role a module declares is the promise "grant this person this role and my
// screens open for them". Nothing can keep that promise unless the declaration
// is checked before it is seeded, so every refusal here is the composition
// refusing a lie rather than a runtime refusing a person: boot and bootstrap
// fail before the database opens, which is the posture checkPersonas already
// took for the composition's own hand-written personas.
//
// Each case is one sentence the manifest could say that would not be true.
func TestValidateRefusesARoleThatGrantsWhatItsModuleDoesNotDeclare(t *testing.T) {
	err := Validate([]Module{{
		Name:        "task",
		Permissions: []Permission{{Key: "task:read", Label: "read tasks"}},
		Roles:       []RoleDecl{{Name: "coordinator", Grants: []string{"task:read", "task:update"}}},
	}})
	if err == nil || !strings.Contains(err.Error(), `role "coordinator" grants "task:update", which this module does not declare`) {
		t.Fatalf("Validate accepted a role granting an undeclared permission:\n%v", err)
	}
}

func TestValidateRefusesARoleThatGrantsAnOperatorPermission(t *testing.T) {
	err := Validate([]Module{{
		Name:        "tenant",
		Permissions: []Permission{{Key: "tenant:manage", Label: "manage tenants", Operator: true}},
		Roles:       []RoleDecl{{Name: "tenant_lead", Grants: []string{"tenant:manage"}}},
	}})
	if err == nil || !strings.Contains(err.Error(), `role "tenant_lead" grants "tenant:manage", an operator permission`) {
		t.Fatalf("Validate accepted a customer role reaching the control plane:\n%v", err)
	}
}

// V3. auth seeds admin (the wildcard) and member (nothing) into every tenant
// itself, with ON CONFLICT DO NOTHING. A module declaring either name would
// write a grant into a row auth already wrote — and the conflict clause would
// make the difference invisible until somebody's role did not do what its
// manifest said.
func TestValidateRefusesAReservedRoleName(t *testing.T) {
	for _, name := range []string{"admin", "member"} {
		err := Validate([]Module{{
			Name:        "task",
			Permissions: []Permission{{Key: "task:read", Label: "read tasks"}},
			Roles:       []RoleDecl{{Name: name, Grants: []string{"task:read"}}},
		}})
		if err == nil || !strings.Contains(err.Error(), `role "`+name+`" is auth's own to seed`) {
			t.Errorf("Validate accepted a module declaring auth's %q role:\n%v", name, err)
		}
	}
}

// V4: an empty grant list is member's shape, and a name the roles route cannot
// answer with is a role nobody can grant from the screen that grants roles. The
// grammar is auth's ValidRoleName, restated in this package because kit/module
// may not import a module's contracts.
func TestValidateRefusesARoleWithNoGrantsOrABadName(t *testing.T) {
	cases := []struct {
		role RoleDecl
		want string
	}{
		{RoleDecl{Name: "observer"}, "grants nothing"},
		{RoleDecl{Name: "Desk Lead", Grants: []string{"task:read"}}, "not a lower-case identifier"},
		{RoleDecl{Name: "x_y", Grants: []string{"task:read"}}, ""},
		{RoleDecl{Name: strings.Repeat("r", maxRoleName+1), Grants: []string{"task:read"}}, "longer than 64 characters"},
	}
	for _, c := range cases {
		err := Validate([]Module{{
			Name:        "task",
			Permissions: []Permission{{Key: "task:read", Label: "read tasks"}},
			Roles:       []RoleDecl{c.role},
		}})
		switch {
		case c.want == "" && err != nil:
			t.Errorf("Validate refused a well-formed role %q:\n%v", c.role.Name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("Validate accepted role %+v; wanted a complaint about %q\n%v", c.role, c.want, err)
		}
	}
}

// V5: two modules seeding one name write two answers into one row, and the
// ON CONFLICT DO NOTHING keeps whichever tenant was created first from ever
// noticing. The name is unique across the composed set, which is what makes a
// granted role mean one thing in every tenant of one installation.
func TestValidateRefusesOneRoleNameDeclaredByTwoModules(t *testing.T) {
	err := Validate([]Module{
		{
			Name:        "task",
			Permissions: []Permission{{Key: "task:read", Label: "read tasks"}},
			Roles:       []RoleDecl{{Name: "observer", Grants: []string{"task:read"}}},
		},
		{
			Name:        "billing",
			Permissions: []Permission{{Key: "invoice:read", Label: "read invoices"}},
			Roles:       []RoleDecl{{Name: "observer", Grants: []string{"invoice:read"}}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `role "observer" is already declared by module "task"`) {
		t.Fatalf("Validate accepted two modules declaring one role name:\n%v", err)
	}
}

func TestValidateRefusesTheSameRoleNameTwiceInOneModule(t *testing.T) {
	decl := RoleDecl{Name: "observer", Grants: []string{"task:read"}}
	err := Validate([]Module{{
		Name:        "task",
		Permissions: []Permission{{Key: "task:read", Label: "read tasks"}},
		Roles:       []RoleDecl{decl, decl},
	}})
	if err == nil || !strings.Contains(err.Error(), `role "observer" is declared twice`) {
		t.Fatalf("Validate accepted a module declaring one role twice:\n%v", err)
	}
}

// The positive half, and the shape modules/task ships: two roles, each grant
// declared by the same manifest that declares the role.
func TestValidateAcceptsRolesThatGrantOnlyWhatTheirModuleOwns(t *testing.T) {
	err := Validate([]Module{
		{
			Name: "task",
			Permissions: []Permission{
				{Key: "task:read", Label: "read tasks"},
				{Key: "task:update", Label: "change tasks"},
			},
			Roles: []RoleDecl{
				{Name: "coordinator", Grants: []string{"task:read", "task:update"}},
				{Name: "observer", Grants: []string{"task:read"}},
			},
			Nav: []NavEntry{{Label: "Tasks", Screen: "task/tasks", Permission: "task:read"}},
		},
		{
			Name:        "billing",
			Permissions: []Permission{{Key: "invoice:read", Label: "read invoices"}},
			Roles:       []RoleDecl{{Name: "invoice_reader", Grants: []string{"invoice:read"}}},
		},
	})
	if err != nil {
		t.Fatalf("Validate refused a well-formed pair of role declarations:\n%v", err)
	}
}
