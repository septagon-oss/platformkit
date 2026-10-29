package module

import (
	"strings"
	"testing"
)

// TestASystemAccessReasonIsOneLineAndASentence bounds the audit sentence. The
// reason reaches a log field and a span attribute and nothing else, so a bound
// is what keeps the audit line a line — and an empty one is not a weaker claim
// but no claim at all, which is why Declared is a method.
func TestASystemAccessReasonIsOneLineAndASentence(t *testing.T) {
	long := "read every tenant's roll-up, which belongs to no tenant " + strings.Repeat("and read it again ", 20)
	for name, access := range map[string]SystemAccess{
		"too long":         {Reason: long},
		"spans lines":      {Reason: "the sweep runs at night\nand deletes what it finds"},
		"spans columns":    {Reason: "the sweep runs\tat night"},
		"one sentence":     {Reason: "sweep stored bytes no row references, across every tenant"},
		"declares nothing": {},
	} {
		bad := access.fault()
		switch name {
		case "one sentence":
			if bad != "" {
				t.Errorf("a reason in one sentence is refused: %s", bad)
			}
		case "declares nothing":
			if bad != "" || access.Declared() {
				t.Errorf("an empty reason is treated as a declaration: fault %q, declared %v", bad, access.Declared())
			}
		default:
			if bad == "" {
				t.Errorf("the reason that %s is accepted", name)
			}
		}
	}
	if (SystemAccess{Reason: "   "}).Declared() {
		t.Error("a reason of spaces declares cross-tenant work")
	}
}

func TestASystemAccessReasonThatSpansLinesRefusesTheManifest(t *testing.T) {
	m := Module{Name: "task", SystemAccess: SystemAccess{Reason: "read every tenant\nbecause it is easier"}}
	err := Validate([]Module{m})
	if err == nil || !strings.Contains(err.Error(), "module \"task\"") || !strings.Contains(err.Error(), "spans lines") {
		t.Fatalf("the manifest with a multi-line reason was not refused naming the module: %v", err)
	}
}

func TestAModuleThatDeclaresNoCrossTenantWorkIsValid(t *testing.T) {
	if err := Validate([]Module{{Name: "task"}}); err != nil {
		t.Fatalf("the zero SystemAccess says the module crosses no tenant boundary: %v", err)
	}
}
