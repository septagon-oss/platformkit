package exampletest

import "testing"

func TestExampleFramesRequireOneSuccessfulExecution(t *testing.T) {
	const run = "=== RUN   ExampleOne\n"
	const pass = "--- PASS: ExampleOne (0.01s)\n"
	if err := checkFrames("PASS\n", nil); err == nil {
		t.Fatal("an empty example list accepted a package PASS")
	}
	for _, tc := range []struct {
		name, output string
		valid        bool
	}{
		{"executed", run + pass + "PASS\n", true},
		{"no tests", "testing: warning: no tests to run\nPASS\n", false},
		{"missing run", pass, false},
		{"missing pass", run, false},
		{"skip", run + "--- SKIP: ExampleOne (0.01s)\n", false},
		{"failure", run + "--- FAIL: ExampleOne (0.01s)\n", false},
		{"duplicate run", run + run + pass, false},
		{"duplicate pass", run + pass + pass, false},
		{"out of order", pass + run, false},
		{"wrong name", "=== RUN   ExampleOther\n--- PASS: ExampleOther (0.01s)\n", false},
		{"malformed pass", run + "--- PASS: ExampleOne nonsense\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkFrames(tc.output, []string{"ExampleOne"})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
}
