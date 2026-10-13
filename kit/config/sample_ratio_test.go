package config_test

// The loader refuses an out-of-range sample ratio
// (kit/config/config.go: "telemetry.sample_ratio is %v; it is a fraction of the traces this process
// starts, so between 0 and 1") and every other refusal in this loader names a case — TestLevelAndURLsAreValidated,
// TestTheRedirectPathHasToBeUnderTheAuthPrefix, TestAnInvalidOverrideIsRefusedByName,
// TestNATSRefusesUnsafeOrAmbiguousSettingsWithoutEchoingSecrets — while no test anywhere in the repository
// named `sample_ratio` before this one (grep -rn "sample_ratio" --include=*_test.go . → nothing). That
// asymmetry matters here more than elsewhere: this key decides how much of a tenant's traffic is measured
// at all, and a value the loader accepted but the sampler re-read differently would answer "how many of
// this tenant's traces do we keep?" with two numbers. The case is written against the file an operator
// copies, edited the way they would edit it.

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
)

func TestTheSampleRatioIsAFractionOrTheBootRefusesIt(t *testing.T) {
	for _, tt := range []struct {
		value string
		ok    bool
	}{
		{"0", true}, // 0 is an answer: keep none of what this process starts
		{"1", true}, // and 1 is the other answer
		{"0.25", true},
		{"-0.5", false}, // 1.5 and -0.5 are typos, not a rounding of the ends
		{"1.5", false},
		{"100", false}, // a percent, which is the other thing a person means by "ratio"
	} {
		t.Run(tt.value, func(t *testing.T) {
			cfg := withRatio(t, tt.value)
			got, err := config.Load(cfg)
			if tt.ok {
				if err != nil {
					t.Fatalf("Load refused telemetry.sample_ratio %s: %v", tt.value, err)
				}
				if got.Telemetry.SampleRatio == nil || *got.Telemetry.SampleRatio != mustFloat(tt.value) {
					t.Errorf("sample_ratio %s arrived as %v", tt.value, got.Telemetry.SampleRatio)
				}
				if r := got.Telemetry.Ratio(); r != mustFloat(tt.value) {
					t.Errorf("Ratio() answered %v for the file's %s: the value the loader read and the value the sampler is handed are one question", r, tt.value)
				}
				return
			}
			if err == nil {
				t.Fatalf("Load accepted telemetry.sample_ratio %s, which is not a fraction of anything", tt.value)
			}
			if !strings.Contains(err.Error(), "sample_ratio") {
				t.Errorf("the refusal does not name the key the operator mistyped: %v", err)
			}
		})
	}

	// The key stays unreadable from the environment, as its own comment promises: a sampling decision is
	// reviewed with the file, and an environment variable that could flip it is a second writer.
	t.Setenv("PLATFORMKIT_TELEMETRY_SAMPLE_RATIO", "0.1")
	got, err := config.Load(withRatio(t, "1"))
	if err != nil {
		t.Fatalf("Load with an unrelated environment variable: %v", err)
	}
	if got.Telemetry.Ratio() != 1 {
		t.Errorf("the environment moved the sample ratio to %v, which the key's own comment says it may not do", got.Telemetry.Ratio())
	}
}

// withRatio is config.example.yaml with its commented `# sample_ratio: 1.0` line written as a real key, so
// the only difference from the file the README tells a reader to copy is the number under test.
func withRatio(t *testing.T, value string) string {
	t.Helper()
	body, err := os.ReadFile(example)
	if err != nil {
		t.Fatalf("read %s: %v", example, err)
	}
	const commented = "  # sample_ratio: 1.0"
	out := strings.Replace(string(body), commented, "  sample_ratio: "+value, 1)
	if out == string(body) {
		t.Fatal("the sample_ratio line of config.example.yaml has moved; this case edits it by hand")
	}
	path := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatalf("write the config: %v", err)
	}
	return path
}

func mustFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(err)
	}
	return f
}
