package billing_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/billing"
)

// TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence: the money answer is the one port this
// module cannot invent, so a Deps without it is refused by name and with the way to
// correct it.
func TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence(t *testing.T) {
	rows := []struct {
		name string
		deps billing.Deps
		want string
	}{
		{"no payment processor", billing.Deps{},
			"billing.Module: Deps.Payments is required"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			_, _, err := billing.NewChecked(r.deps)
			if err == nil {
				t.Fatal("NewChecked built a plan answer with no way to take money")
			}
			if !strings.Contains(err.Error(), r.want) {
				t.Errorf("refusal = %q, want it to contain %q", err, r.want)
			}
			var got any
			func() {
				defer func() { got = recover() }()
				billing.New(r.deps)
			}()
			if s, ok := got.(string); !ok || s != err.Error() {
				t.Errorf("panicked with %#v, want the string the checked form answered %q", got, err)
			}
		})
	}
}

// TestNewCheckedBuildsFromItsRequiredDependenciesAlone: the manual processor is
// this module's own answer when no payment module is composed beside it.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	plans, m, err := billing.NewChecked(billing.Deps{Payments: billing.Manual()})
	if err != nil {
		t.Fatalf("NewChecked with the manual processor: %v", err)
	}
	if plans == nil {
		t.Fatal("the plan answer is nil")
	}
	if m.Name != "billing" {
		t.Errorf("manifest name = %q, want billing", m.Name)
	}
}
