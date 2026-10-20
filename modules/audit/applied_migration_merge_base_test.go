package audit_test

import (
	"os/exec"
	"testing"
)

func TestAppliedMigrationGateComparesTheMergeBase(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "bash", "../../scripts/applied_migration_merge_base_test.sh")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("a topic with no migration change must pass with an explicit upstream base: %v\n%s", err, out)
	}
}
