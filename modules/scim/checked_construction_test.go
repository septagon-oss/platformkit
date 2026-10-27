package scim_test

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

// roleDirectory and authority answer one question each and are never called: the
// refusals below happen before a provisioning request exists. Both ports are this
// module's own, so no shared fake carries them.
type roleDirectory struct{}

func (roleDirectory) HasRole(context.Context, db.Tx[db.Tenant], string) (bool, error) {
	return false, nil
}

type authority struct{}

func (authority) Holds(context.Context, db.Tx[db.Tenant], string) (bool, error) {
	return false, nil
}

// TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence: each of the three answers a write that
// would otherwise look like success — a half-imported directory, a group bound to a
// role that does not exist, a role grant nobody was asked about.
func wired() scim.Deps {
	return scim.Deps{Provisioning: usertest.NewFake(), Roles: roleDirectory{}, Authority: authority{}}
}
func TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence(t *testing.T) {
	rows := []struct {
		name string
		deps scim.Deps
		want string
	}{
		{"nobody to provision", scim.Deps{},
			"scim.Module: Deps.Users is required"},
		{"no role names to bind a group to", scim.Deps{Provisioning: usertest.NewFake()}, "scim.Module: Deps.Roles is required"},
		{"nobody to ask who is calling", scim.Deps{Provisioning: usertest.NewFake(), Roles: roleDirectory{}},
			"scim.Module: Deps.Authority is required"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			_, _, err := scim.NewChecked(r.deps)
			if err == nil {
				t.Fatal("NewChecked built a provisioning door with one leg missing")
			}
			if !strings.Contains(err.Error(), r.want) {
				t.Errorf("refusal = %q, want it to contain %q", err, r.want)
			}
			var got any
			func() {
				defer func() { got = recover() }()
				scim.New(r.deps)
			}()
			if s, ok := got.(string); !ok || s != err.Error() {
				t.Errorf("panicked with %#v, want the string the checked form answered %q", got, err)
			}
		})
	}
}

// TestNewCheckedBuildsFromItsRequiredDependenciesAlone: three ports and nothing
// else — no page, no job, no subscription — is the whole of this module.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	svc, m, err := scim.NewChecked(wired())
	if err != nil {
		t.Fatalf("NewChecked with the three ports: %v", err)
	}
	if svc == nil {
		t.Fatal("the provisioning service is nil")
	}
	if m.Name != "scim" {
		t.Errorf("manifest name = %q, want scim", m.Name)
	}
}
