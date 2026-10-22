package ui_test

import (
	"os/exec"
	"testing"
)

func TestTheDesignFloorRefusesEachBrokenBoundary(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "node", "testdata/design_floor_cases.mjs")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("design floor boundaries: %v\n%s", err, out)
	}
}
