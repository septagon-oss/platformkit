package main

// Two lines this branch leans on that no other case in this package
// reads.
//
// The defect this holds shut — a catalog body whose type was erased, published as
// {"schema":{}} while every gate stayed green — is cured and stays cured: its
// mutation was re-run at e81c9f7 and now fails three cases, naming each lost key.
// What is still unpinned sits one line further up.
//
// apps/platformkit/app_test.go's start() fills a nil opts.WorkspaceCatalog with
// the product's mount, so every case here — both contract gates and this pin
// included — boots a process that answers /api/v1/app/resources whatever the
// composition does. With `WorkspaceCatalog: workspaceCatalog(),` deleted from
// appOptions the whole package is green:
//
// 	go test ./apps/platformkit -count=1        -> ok   (measured at e81c9f7: 57.922s, rc=0)
//
// The process run() and serve() actually start does fail, loudly: the kernel's
// composeGates (kit/app/app.go, reached from Run through buildAPI) refuses a
// composition with resources and no renderer, and scripts/e2e.sh and
// scripts/mobile_e2e.sh both boot the built binary and stop when it will not serve.
// So this is not a contract lost quietly — it is a gap in the gate that runs before
// a commit: fault.go's claim that "the document a gate compares against is the one
// this file wires" is held by nothing that runs on `make check`.
//
// The assertions below reach themselves through what correct behaviour prints — a
// wired field, an accepted composition, and which report shapes may count as a pass.
// None of them depends on the wording of a refusal, a redirect, or a branch the
// fixed code does not take.

import (
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
)

// TestTheCompositionMountsTheCatalogItsContractIsGeneratedFrom refuses a
// composition that stops mounting the catalog and lets the test harness supply it.
func TestTheCompositionMountsTheCatalogItsContractIsGeneratedFrom(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)

	options := appOptions(cfg, c, app.All)
	if options.WorkspaceCatalog == nil {
		t.Error("appOptions wires no WorkspaceCatalog: start() mounts the product's own, so the contract gates in this package read a document the composition does not publish")
	}
	// And the options as this file wrote them are the ones the kernel accepts, so
	// the line above is a claim about the catalog mount and not a side effect of
	// some other wiring mistake. (The catalog mount's own refusal belongs to Run;
	// this half is what makes the first half mean what it says.)
	if _, err := app.New(t.Context(), cfg, c.modules, options); err != nil {
		t.Errorf("app.New refuses the options apps/platformkit/fault.go writes: %v", err)
	}
}

// TestAReportThatNamesNoPassingFlowIsNotAPass replays the shapes a Maestro report
// arrives in and pins which of them may count toward mobile_flow_pass_rate. The
// report is the only evidence a device journey happened; a skipped case, a failed
// one and a report that never mentions the flow must not read as the pass that
// number is a quotient of.
func TestAReportThatNamesNoPassingFlowIsNotAPass(t *testing.T) {
	suite := func(fails string, cases, skipped int, failAttr, body string) string {
		return fmt.Sprintf(`<testsuites name="Maestro suite" tests="1" failures="%s">
  <testsuite name="catalog" tests="%d" skipped="%d" failures="%s" errors="0" time="33.0">
    %s
  </testsuite>
</testsuites>`, fails, cases, skipped, failAttr, body)
	}
	passed := `<testcase classname="catalog" name="catalog.yaml" time="33.0"/>`
	for _, tc := range []struct {
		name       string
		report     string
		wantPassed bool
		wantState  string
	}{
		{
			name:       "the flow ran and passed",
			report:     suite("0", 1, 0, "0", passed),
			wantPassed: true, wantState: "passed",
		},
		{
			name:      "the flow ran and failed",
			report:    suite("1", 1, 0, "1", `<testcase classname="catalog" name="catalog.yaml" time="33.0"><failure message="step failed">x</failure></testcase>`),
			wantState: "failed",
		},
		{
			name:      "the flow was skipped",
			report:    suite("0", 1, 1, "0", `<testcase classname="catalog" name="catalog.yaml" time="0.0"><skipped/></testcase>`),
			wantState: "skipped",
		},
		{
			name:      "the report never mentions the flow",
			report:    suite("0", 0, 0, "0", ""),
			wantState: "no test cases",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			results := readMaestroJUnit(t, []byte(tc.report))
			got, ok := results["catalog"]
			if !ok {
				t.Fatalf("the report yields no outcome for catalog, the flow this repository declares: %v", results)
			}
			if got.passed != tc.wantPassed {
				t.Errorf("mobile_flow_pass_rate would count this report as passed=%v, want passed=%v (state %q)", got.passed, tc.wantPassed, got.state)
			}
			if got.state != tc.wantState {
				t.Errorf("the report's flow reads as state %q, want %q", got.state, tc.wantState)
			}
		})
	}
}
