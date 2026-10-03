package main

// The denominator of mobile_flow_pass_rate, pinned.
//
// The brief defines the number: "declared mobile flows whose Maestro spec ran and
// passed ÷ declared". e2e/maestro/flows.json states the same thing about itself —
// "the ratio mobile_flow_pass_rate is a quotient of this file and the JUnit the
// mobile job writes, and not a number somebody remembers" — and
// e2e/maestro/README.md repeats it: "[flows.json] is the declared set of mobile
// device flows — the denominator of mobile_flow_pass_rate, written down rather than
// remembered".
//
// TestDeclaredMobileFlowsRan divides by something else. It `continue`s past every
// flow whose `runs` is not "platformkit" before counting, so its denominator is the
// number of flows this repository owns — 1 — and the moment the one flow passes the
// line reads `mobile_flow_pass_rate = 1/1`, a clean 100% with four declared flows
// still run by hand in a repository this job never touches. The command, against a
// report in which the kernel's flow passed:
//
// 	printf '<testsuites><testsuite name="catalog" tests="1"><testcase name="e2e/maestro/catalog.yaml"/></testsuite></testsuites>' >/tmp/pass.xml
// 	MAESTRO_JUNIT=/tmp/pass.xml go test ./apps/platformkit -run TestDeclaredMobileFlowsRan -count=1 -v
// 	    -> mobile_flow_pass_rate = 1/1 over the flows this repository runs
//
// while the file the same commit calls the denominator lists five flows.
//
// The case below asserts the weaker, durable half of the sentence — the number this
// delivery named mobile_flow_pass_rate divides by the declared set it claims to
// divide by — and leaves the *threshold* to the delivery: a fix may report 1/5 as a
// refusal that names the four hand-run flows, or 1/5 as a logged ratio, or refuse
// the name altogether and call the owned share something else, so long as the ratio
// it reports over the manifest has five beneath the line. It fails today because
// one is beneath it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestTheMobileFlowRateDividesByTheDeclaredSet(t *testing.T) {
	flows := readMobileFlows(t)
	declared := len(flows.Flows)
	if declared < 2 {
		t.Fatalf("%s declares %d flows; this case needs a declared set larger than the one this repository runs to mean anything", mobileFlowManifest, declared)
	}

	// A report in which the flow this repository owns ran and passed: the best
	// report a journey of this size can produce, and the one that decides the number.
	dir := t.TempDir()
	report := filepath.Join(dir, "maestro.xml")
	body := `<testsuites><testsuite name="catalog" tests="1"><testcase name="e2e/maestro/catalog.yaml"/></testsuite></testsuites>`
	if err := os.WriteFile(report, []byte(body), 0o644); err != nil {
		t.Fatalf("write the report: %v", err)
	}

	cmd := exec.Command("go", "test", "./apps/platformkit", "-v",
		"-run", "TestDeclaredMobileFlowsRan", "-count=1")
	cmd.Dir = "../.." // the repository root, which is where ./apps/platformkit is
	cmd.Env = append(os.Environ(), "MAESTRO_JUNIT="+report)
	out, err := cmd.CombinedOutput()

	rate := regexp.MustCompile(`mobile_flow_pass_rate = (\d+)/(\d+)`).FindSubmatch(out)
	if rate == nil {
		t.Fatalf("the run reported no mobile_flow_pass_rate at all (err=%v):\n%s", err, out)
	}
	passed, _ := strconv.Atoi(string(rate[1]))
	divisor, _ := strconv.Atoi(string(rate[2]))
	if passed != 1 {
		t.Fatalf("the report says the kernel's own flow passed, and the rate says %d passed (err=%v):\n%s", passed, err, out)
	}
	if divisor != declared {
		t.Errorf("mobile_flow_pass_rate is reported as %d/%d, but %s declares %d flows and calls itself that number's denominator: a rate over the flows this repository happens to run is the number somebody remembers, which is what the manifest says it exists to stop",
			passed, divisor, mobileFlowManifest, declared)
	}
}
