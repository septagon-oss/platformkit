package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestOverallCoverageCannotFallWhenEveryPageKeepsItsRatio(t *testing.T) {
	if scenario := os.Getenv("PKIT_COVERAGE_SCENARIO"); scenario != "" {
		wanted := report{Version: floorVersion, Pages: map[string]coverage{
			"GET /translated": {Wrapped: 1, Readable: 1},
			"GET /literal":    {Wrapped: 0, Readable: 1},
		}}
		got := report{Version: floorVersion, Pages: map[string]coverage{
			"GET /translated": {Wrapped: 1, Readable: 1},
			"GET /literal":    {Wrapped: 0, Readable: 1},
		}}
		switch scenario {
		case "same":
		case "page-fell":
			got.Pages["GET /translated"] = coverage{Wrapped: 0, Readable: 1}
		case "overall-fell":
			// More literal copy on a zero-coverage page keeps that page at zero,
			// but lowers the whole application's coverage from 1/2 to 1/3.
			got.Pages["GET /literal"] = coverage{Wrapped: 0, Readable: 2}
		default:
			t.Fatalf("unknown coverage scenario %q", scenario)
		}
		check(t, wanted, got, nil, nil, nil)
		return
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"same", "page-fell", "overall-fell"} {
		t.Run(scenario, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), bin, "-test.run=^TestOverallCoverageCannotFallWhenEveryPageKeepsItsRatio$")
			cmd.Env = append(os.Environ(), "PKIT_COVERAGE_SCENARIO="+scenario)
			out, err := cmd.CombinedOutput()
			if !strings.Contains(string(out), "i18n coverage ") {
				t.Fatalf("the child did not measure coverage: %v\n%s", err, out)
			}
			if scenario == "same" && err != nil {
				t.Fatalf("an unchanged measurement was refused: %v\n%s", err, out)
			}
			if scenario != "same" && err == nil {
				t.Errorf("the gate accepted a coverage regression (%s):\n%s", scenario, out)
			}
		})
	}
}
