package events_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTopicPayloadTypeIsCheckedByCompiler(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path is unavailable")
	}
	for _, fixture := range []struct {
		name string
		good bool
	}{
		{name: "correctpayload", good: true},
		{name: "wrongpayload", good: false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "fixture")
			cmd := exec.Command("go", "build", "-o", out, "./testdata/"+fixture.name)
			cmd.Dir = filepath.Dir(source)
			result, err := cmd.CombinedOutput()
			if fixture.good {
				if err != nil {
					t.Fatalf("correct payload did not compile: %v\n%s", err, result)
				}
				return
			}
			if err == nil {
				t.Fatal("wrong payload compiled successfully")
			}
			if !strings.Contains(string(result), "in call to events.PublishTo") || !strings.Contains(string(result), "PayloadB") {
				t.Fatalf("compiler failure was not the topic payload type mismatch: %v\n%s", err, result)
			}
		})
	}
}
