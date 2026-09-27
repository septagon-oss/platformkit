package main

// Review 10's pin. The deliverable the brief names is "a command the operator runs", and every
// committed case reaches the repair by calling repairRoles([]string{...}) from Go. Nothing in the
// tree runs the binary, so the one case in main's switch is the line of that deliverable no test
// touches: delete it, or spell it "repair_roles", and the repair ships unreachable with every
// case green. So this builds the command once and asks it the two questions the switch answers.
// -buildvcs=false is the `make build` goal's own flag, kept for the same reason it keeps it: the
// .git of a linked worktree is a file, and stamping a binary nothing keeps is not what this is for.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheRepairRolesSubcommandIsReachableFromTheBinary(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "platformkit")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the command: %v\n%s", err, out)
	}
	run := func(args ...string) (string, error) {
		out, err := exec.Command(binary, args...).CombinedOutput()
		return string(out), err
	}

	// A mistyped name prints the sentence listing every command there is, which is how a person
	// learns the repair exists. Both halves are pinned: the sentence, and the failed exit.
	usage, err := run("prune")
	if err == nil || !strings.Contains(usage, "is not a command") || !strings.Contains(usage, "repair-roles") {
		t.Fatalf("`platformkit prune` printed %q (err %v), want the usage sentence naming repair-roles", usage, err)
	}

	// The probe, and the half the sentence above cannot prove: the default branch prints its
	// sentence for any name at all, so reachability is shown by the refusal only the repair's own
	// case can print — the configuration loader's, from inside repairRoles — and not reached here
	// through anything the missing case would have printed.
	absent := filepath.Join(t.TempDir(), "absent.yaml")
	out, err := run("repair-roles", "--config", absent)
	if err == nil || !strings.Contains(out, "does not exist") || strings.Contains(out, "is not a command") {
		t.Fatalf("`platformkit repair-roles --config %s` printed %q (err %v); want the loader's refusal"+
			", which only the repair's own case in main's switch reaches", absent, out, err)
	}
}
