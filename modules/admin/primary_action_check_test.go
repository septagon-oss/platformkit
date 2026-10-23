package admin

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	admincontracts "github.com/septagon-oss/platformkit/modules/admin/contracts"
)

// TestAPrimaryActionIsCheckedAgainstTheComposition is the one decision wire takes
// about the port, before anything opens: nothing declared is no action, a whole
// declaration comes back as it was, and an action with no words, no permission,
// or a permission no composed module defines refuses the plan, naming what it
// lacks.
func TestAPrimaryActionIsCheckedAgainstTheComposition(t *testing.T) {
	mods := []module.Module{{Name: "task", Permissions: []module.Permission{{Key: "task:read", Label: "read tasks"}}}}
	whole := admincontracts.PrimaryAction{Label: "Go to tasks", Screen: "task/tasks", Permission: "task:read"}
	for _, tc := range []struct {
		name    string
		in      admincontracts.PrimaryAction
		want    module.NavEntry
		refusal []string
	}{
		{name: "nothing declared draws nothing", in: admincontracts.PrimaryAction{}},
		{name: "a whole declaration is the action", in: whole, want: module.NavEntry(whole)},
		{name: "an action with no words", in: admincontracts.PrimaryAction{Screen: "task/tasks", Permission: "task:read"}, refusal: []string{`"task/tasks"`, "no label"}},
		{name: "an action with no permission", in: admincontracts.PrimaryAction{Label: "Go to tasks", Screen: "task/tasks"}, refusal: []string{`"Go to tasks"`, "declares no permission"}},
		{name: "a permission nobody defines", in: admincontracts.PrimaryAction{Label: "Go to tasks", Screen: "task/tasks", Permission: "nope:read"}, refusal: []string{`"Go to tasks"`, `"nope:read"`, "which no module defines"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := primaryAction(tc.in, mods)
			if tc.refusal == nil {
				if err != nil || got != tc.want {
					t.Fatalf("primaryAction(%+v) = %+v, %v; want %+v and no refusal", tc.in, got, err, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("primaryAction(%+v) = %+v and no refusal", tc.in, got)
			}
			if got != (module.NavEntry{}) {
				t.Errorf("a refused action still came back as %+v", got)
			}
			for _, want := range tc.refusal {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal %q does not say %s", err, want)
				}
			}
		})
	}
}
