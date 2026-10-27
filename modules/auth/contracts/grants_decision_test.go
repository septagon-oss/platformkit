package contracts_test

import (
	"errors"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestDecideReadsOnlyThePrincipalsUnboundedRoles(t *testing.T) {
	id := uuid.New()
	for _, p := range []tenancy.Principal{
		{}, {Roles: []string{"owner"}}, {UserID: id},
		{UserID: id, Roles: []string{"owner"}, Permissions: []string{}},
		{UserID: id, Roles: []string{"owner"}, Permissions: []string{"widget:read"}},
	} {
		calls := 0
		_, err := authcontracts.Decide(p, tenancy.Grant{Permission: "role:manage"}, func([]string) ([]string, error) { calls++; return []string{"*"}, nil })
		if err != nil || calls != 0 {
			t.Fatalf("principal %+v read roles %d times: %v", p, calls, err)
		}
	}
	roles := []string{"editor", "editor"}
	calls := 0
	sentinel := errors.New("role store unavailable")
	ok, err := authcontracts.Decide(tenancy.Principal{UserID: id, Roles: roles}, tenancy.Grant{Permission: "widget:read"}, func(got []string) ([]string, error) {
		calls++
		if !slices.Equal(got, roles) {
			t.Fatalf("roles = %v", got)
		}
		return nil, sentinel
	})
	if ok || !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("read failure = %v, %v; calls=%d", ok, err, calls)
	}
	if !slices.Equal(roles, []string{"editor", "editor"}) {
		t.Fatal("roles were mutated")
	}
}

func TestDecideRefusesBlankGrantsAndMissingReaders(t *testing.T) {
	p := tenancy.Principal{UserID: uuid.New(), Roles: []string{"editor"}}
	for _, want := range []tenancy.Grant{{}, {Permission: " \t"}, {Permission: "widget:read"}} {
		if ok, err := authcontracts.Decide(p, want, nil); ok || err == nil {
			t.Fatalf("invalid decision = %v, %v", ok, err)
		}
	}
	if !authcontracts.Match([]string{"fleet:manage"}, tenancy.Grant{Permission: "fleet:manage", Operator: true}) {
		t.Fatal("explicit operator permission was lost")
	}
}
