package user_test

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence: two doors guard the two writes that can
// lock a tenant out — emptying someone's roles and deleting their row — and an
// adapter that answers nothing answers "nobody administers here", which is the
// missing floor wearing a wiring.
func both() user.Deps {
	return user.Deps{
		Administration: &contracts.AdministrationFunc{
			Ask: func(context.Context, db.Tx[db.Tenant]) ([]string, error) { return nil, nil },
		},
		Granting: &contracts.GrantingFunc{
			Ask: func(context.Context, db.Tx[db.Tenant]) (bool, error) { return false, nil },
		},
	}
}
func TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence(t *testing.T) {
	rows := []struct {
		name string
		deps user.Deps
		want string
	}{
		{"no administration floor", user.Deps{},
			"user.Module: Deps.Administration is required"},
		{"an administration adapter with no Ask", user.Deps{Administration: &contracts.AdministrationFunc{}}, "user.Module: Deps.Administration is an adapter with no Ask"},
		{"a nil administration adapter wearing the type", user.Deps{Administration: (*contracts.AdministrationFunc)(nil)}, "user.Module: Deps.Administration is an adapter with no Ask"},
		{"no granting door", user.Deps{Administration: both().Administration}, "user.Module: Deps.Granting is required"},
		{"a granting adapter with no Ask", user.Deps{Administration: both().Administration, Granting: &contracts.GrantingFunc{}}, "user.Module: Deps.Granting requires an initialized adapter"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			_, _, err := user.NewChecked(r.deps)
			if err == nil {
				t.Fatal("NewChecked built the people desk with one of its doors unwired")
			}
			if !strings.Contains(err.Error(), r.want) {
				t.Errorf("refusal = %q, want it to contain %q", err, r.want)
			}
			var got any
			func() {
				defer func() { got = recover() }()
				user.New(r.deps)
			}()
			if s, ok := got.(string); !ok || s != err.Error() {
				t.Errorf("panicked with %#v, want the string the checked form answered %q", got, err)
			}
		})
	}
}

// TestNewCheckedBuildsFromItsRequiredDependenciesAlone: the two Ask closures are the
// whole of what a composition owes this module; the desk needs no database to be
// built, only someone to answer the two questions.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	svc, m, err := user.NewChecked(both())
	if err != nil {
		t.Fatalf("NewChecked with both doors answered: %v", err)
	}
	if svc == nil {
		t.Fatal("the people service is nil")
	}
	if m.Name != "user" {
		t.Errorf("manifest name = %q, want user", m.Name)
	}
}
