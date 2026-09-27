package contenttest_test

import (
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/porttest"
)

// TestMain exists for one line of review 1's mutation proof: it runs this
// package's suite a second time through testing.RunTests, whose nested runner
// writes the same framing lines the framework writes, so the mutant the suite
// refuses arrives as a failing test named contenttest_RunService that no test
// file contains and make check fails on it. porttest.NestedRuns takes that claim
// off the nested runner's lines and nothing else's.
func TestMain(m *testing.M) { os.Exit(porttest.NestedRuns(m, "contenttest_RunService")) }
