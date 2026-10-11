// The declared mobile flows, and the number the brief asks this delivery to move:
// mobile_flow_pass_rate — declared flows whose Maestro spec ran and passed divided
// by declared flows.
//
// The register that was supposed to hold it, tools/pillars.py, does not exist in
// this repository. What exists is the shape apps/platformkit/asyncapi_test.go uses
// for event_schema_coverage: the ratio is computed by a test and refused below its
// target, not written into a document somebody may read.
//
// Two cases, because the two halves have different homes. The declaration is
// checked by make check on every machine: a flow that names no spec, or names one
// this repository does not have, is a flow nobody runs and nobody says so. Whether
// it *ran* can only be known by the job that ran it, which passes MAESTRO_JUNIT; the
// harness refuses a declared flow of this repository's that is missing, failed or
// skipped, and the CI job carries no continue-on-error, so a journey that did not
// pass is a red job rather than a green one with a skipped line in it.

package main

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const mobileFlowManifest = "../../e2e/maestro/flows.json"

// mobileRoot is the repository root, as this package's test working directory sees
// it. A flow's spec is written from the repository root — it is what the harness
// passes to maestro after it cds there — so reading one from a test means saying so.
const mobileRoot = "../../"

// mobileFlows is the declared set.
type mobileFlows struct {
	Schema string `json:"schema"`
	Flows  []struct {
		ID   string `json:"id"`
		Spec string `json:"spec"`
		Runs string `json:"runs"`
	} `json:"flows"`
}

// mobileOwnRunner is the repository whose specs this job runs and whose report it
// reads. The manifest's other value, platformkit-mobile, names a repository this
// loop has no checkout of: a flow declared there is counted in the rate and is never
// opened from here, which is why the case below reads a spec file only for a flow
// this repository owns.
const mobileOwnRunner = "platformkit"

// mobileKnownRunners is the vocabulary the manifest's `runs` may name. It says
// nothing about this machine: mobileOwnRunner's specs are the only ones a case here
// can open.
var mobileKnownRunners = []string{mobileOwnRunner, "platformkit-mobile"}

func readMobileFlows(t *testing.T) mobileFlows {
	t.Helper()
	body, err := os.ReadFile(mobileFlowManifest)
	if err != nil {
		t.Fatalf("read %s: %v", mobileFlowManifest, err)
	}
	var flows mobileFlows
	if err := json.Unmarshal(body, &flows); err != nil {
		t.Fatalf("%s is not JSON: %v", mobileFlowManifest, err)
	}
	if flows.Schema != "platformkit.mobile-flows.v1" {
		t.Errorf("%s says schema %q", mobileFlowManifest, flows.Schema)
	}
	if len(flows.Flows) == 0 {
		t.Fatal("no mobile flow is declared, which makes the ratio a division by zero rather than a metric")
	}
	return flows
}

// TestDeclaredMobileFlowsAreDeclared is the half that owes nothing to a device:
// every flow has an id, a spec and an owning repository this loop knows, no id is
// said twice, and a spec this repository owns is a file that exists here.
func TestDeclaredMobileFlowsAreDeclared(t *testing.T) {
	t.Parallel()
	flows := readMobileFlows(t)
	var owned, ids []string
	for _, flow := range flows.Flows {
		if flow.ID == "" || flow.Spec == "" || flow.Runs == "" {
			t.Errorf("%q: a flow needs an id, a spec and the repository that runs it: %+v", flow.ID, flow)
			continue
		}
		if !slices.Contains(mobileKnownRunners, flow.Runs) {
			t.Errorf("%s names runs %q, which is not a repository this loop has", flow.ID, flow.Runs)
		}
		if slices.Contains(ids, flow.ID) {
			t.Errorf("%s is declared twice, so the ratio would count it twice", flow.ID)
		}
		ids = append(ids, flow.ID)
		if flow.Runs != mobileOwnRunner {
			continue
		}
		owned = append(owned, flow.ID)
		body, err := os.ReadFile(mobileRoot + flow.Spec)
		if err != nil {
			t.Errorf("%s names spec %s: %v", flow.ID, flow.Spec, err)
			continue
		}
		// The appId is the verification profile, said in the flow and not by the
		// harness: a flow that could be aimed at any package would test somebody
		// else's build.
		if !strings.Contains(string(body), "dev.septagon.platformkit.ci") {
			t.Errorf("%s does not name the verification profile's appId", flow.ID)
		}
	}
	if len(owned) == 0 {
		t.Error("no flow is declared with runs: platformkit, so nothing in this repository's CI moves the rate")
	}
}

// TestDeclaredMobileFlowsRan is the number. It reads the JUnit the mobile job wrote
// and refuses a flow this repository declares that is missing, failed or skipped.
// MAESTRO_JUNIT is set by make mobile-e2e and by the CI job, never by hand: the
// harness has already refused the same thing by then, and this is where the rate
// itself is recorded rather than the single pass.
//
// The denominator is every flow the manifest declares, which is what the manifest
// says it is ("the ratio mobile_flow_pass_rate is a quotient of this file and the
// JUnit the mobile job writes") and what the brief names ("declared mobile flows
// whose Maestro spec ran and passed ÷ declared"). Counting only the flows this
// repository runs would report 1/1 the moment its own flow passed — a clean hundred
// percent with four declared flows still run by hand in a repository this job never
// touches, which is the number somebody remembers the manifest exists to stop. The
// owned share is refused; the rest of the denominator is named as what it is, because
// this repository cannot gate a spec it does not own.
func TestDeclaredMobileFlowsRan(t *testing.T) {
	path := os.Getenv("MAESTRO_JUNIT")
	if path == "" {
		t.Skip("MAESTRO_JUNIT is unset: no device run happened here. make mobile-e2e runs one and sets it; the mobile job fails without it.")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the Maestro report %s: %v", path, err)
	}
	results := readMaestroJUnit(t, body)

	flows := readMobileFlows(t)
	declared, owned, passed := len(flows.Flows), 0, 0
	var missing, elsewhere []string
	for _, flow := range flows.Flows {
		if flow.Runs != mobileOwnRunner {
			elsewhere = append(elsewhere, flow.ID)
			continue
		}
		owned++
		result, ok := results[flow.ID]
		switch {
		case !ok:
			missing = append(missing, flow.ID)
		case !result.passed:
			missing = append(missing, flow.ID+" ("+result.state+")")
		default:
			passed++
		}
	}
	t.Logf("mobile_flow_pass_rate = %d/%d", passed, declared)
	if len(missing) > 0 {
		t.Errorf("of the %d declared flows this repository runs, %d passed; these did not: %s",
			owned, passed, strings.Join(missing, ", "))
		return
	}
	if len(elsewhere) > 0 {
		t.Logf("%d of the %d declared flows are owned by another repository and are in no report this job reads: %s",
			len(elsewhere), declared, strings.Join(elsewhere, ", "))
	}
}

// TestTheMobileHarnessRefusesAnUnpinnedShellBuild is the harness refusing to
// download something it cannot name, which is the one refusal in this delivery that
// costs nothing to prove on any machine: it happens before a database is created,
// before a tool is probed and before a device is mentioned.
func TestTheMobileHarnessRefusesAnUnpinnedShellBuild(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		apk     string
		digest  string
		wants   []string
		explain string
	}{
		{"a URL with no digest", "https://example.test/shell.apk", "", []string{"PK_MOBILE_APK", "PK_MOBILE_APK_SHA256"}, "an unpinned binary must never run against a live tenant"},
		{"a digest with no URL", "", strings.Repeat("0", 64), []string{"PK_MOBILE_APK", "PK_MOBILE_APK_SHA256"}, "the pin names nothing"},
		{"a digest that is not one", "https://example.test/shell.apk", "dead" + "beef", []string{"SHA-256"}, "a short digest is a typo, not a pin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "../../scripts/mobile_e2e.sh")
			cmd.Dir = "."
			cmd.Env = append(os.Environ(),
				"PLATFORMKIT_TEST_ADMIN_URL=postgres://postgres:platformkit@localhost:5432/platformkit?sslmode=disable",
				"PLATFORMKIT_TEST_DATABASE_URL=postgres://platformkit_app:platformkit@localhost:5432/platformkit?sslmode=disable",
				"PK_MOBILE_APK="+tc.apk,
				"PK_MOBILE_APK_SHA256="+tc.digest,
			)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("the harness accepted a build it cannot name (%s) and ran anyway:\n%s", tc.explain, out)
			}
			for _, want := range tc.wants {
				if !strings.Contains(string(out), want) {
					t.Errorf("the harness said %q, which does not name %s", out, want)
				}
			}
		})
	}
}

// maestroResult is one flow's outcome, as the report states it.
type maestroResult struct {
	passed bool
	state  string
}

// readMaestroJUnit flattens the report into one outcome per flow. A suite with no
// test cases is an error rather than an empty map: an empty report and a report that
// never mentioned the flow must not read the same way.
func readMaestroJUnit(t *testing.T, body []byte) map[string]maestroResult {
	t.Helper()
	var suites struct {
		XMLName xml.Name `xml:"testsuites"`
		Suites  []struct {
			Name  string `xml:"name,attr"`
			Fail  int    `xml:"failures,attr"`
			Tests []struct {
				Name    string    `xml:"name,attr"`
				Failure *struct{} `xml:"failure"`
				Error   *struct{} `xml:"error"`
				Skipped *struct{} `xml:"skipped"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(body, &suites); err != nil {
		t.Fatalf("%s is not a JUnit report: %v", os.Getenv("MAESTRO_JUNIT"), err)
	}
	out := map[string]maestroResult{}
	for _, suite := range suites.Suites {
		if len(suite.Tests) == 0 {
			// A flow whose file Maestro could not parse arrives as a suite of
			// nothing at all: that is a failure of the flow, not an absence.
			out[suite.Name] = maestroResult{state: "no test cases"}
			continue
		}
		for _, test := range suite.Tests {
			name := maestroFlowID(test.Name)
			switch {
			case test.Failure != nil:
				out[name] = maestroResult{state: "failed"}
			case test.Error != nil:
				out[name] = maestroResult{state: "errored"}
			case test.Skipped != nil:
				out[name] = maestroResult{state: "skipped"}
			default:
				out[name] = maestroResult{passed: true, state: "passed"}
			}
		}
	}
	return out
}

// maestroFlowID is the id a declared flow is known by inside a report line. Maestro
// names a case by the file it came from and its own description, so the join is on
// the file's base name without its extension — which is also the id, by the rule the
// manifest is written under.
func maestroFlowID(name string) string {
	name = strings.TrimSpace(name)
	if slash := strings.LastIndexAny(name, "/\\"); slash >= 0 {
		name = name[slash+1:]
	}
	if dot := strings.LastIndex(name, "."); dot > 0 {
		name = name[:dot]
	}
	return strings.TrimSpace(name)
}
