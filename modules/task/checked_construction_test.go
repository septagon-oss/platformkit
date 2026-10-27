package task_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/task"
)

// TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence: the desk takes one policy. Two answers
// to "may this caller resolve this task" is one too many, and the one that loses
// would be invisible.
func TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence(t *testing.T) {
	rows := []struct {
		name string
		deps task.Deps
		want string
	}{
		{"a policy on the desk and beside it", task.Deps{Service: task.NewService(), Policy: allowAll{}}, "task: configure policy on the shared service or supply Policy, not both"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			_, err := task.NewChecked(r.deps)
			if err == nil {
				t.Fatal("NewChecked built a desk with two policies")
			}
			if !strings.Contains(err.Error(), r.want) {
				t.Errorf("refusal = %q, want it to contain %q", err, r.want)
			}
			var got any
			func() {
				defer func() { got = recover() }()
				task.New(r.deps)
			}()
			if s, ok := got.(string); !ok || s != err.Error() {
				t.Errorf("panicked with %#v, want the string the checked form answered %q", got, err)
			}
		})
	}
}

// TestNewCheckedBuildsFromItsRequiredDependenciesAlone: a policy is optional, and a
// desk without one is the generic one — route grants and the tenant floor.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	m, err := task.NewChecked(task.Deps{})
	if err != nil {
		t.Fatalf("NewChecked with an empty Deps: %v", err)
	}
	if m.Name != "task" {
		t.Errorf("manifest name = %q, want task", m.Name)
	}
}

// TestNewServiceWithPolicyCheckedRefusesNoPolicyProvider: the policy is the reason
// the service exists, and a nil one answers every resource question by allowing it.
func TestNewServiceWithPolicyCheckedRefusesNoPolicyProvider(t *testing.T) {
	if _, err := task.NewServiceWithPolicyChecked(nil); err == nil {
		t.Fatal("a policy-enabled service built with no policy")
	} else if !strings.Contains(err.Error(), "requires a policy provider") {
		t.Errorf("refusal = %q", err)
	}
	if _, err := task.NewServiceWithPolicyChecked(allowAll{}); err != nil {
		t.Errorf("NewServiceWithPolicyChecked with a policy: %v", err)
	}
}
