package file_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestTheCandidateStoreRequiresItsProfileAndSharesTeardown(t *testing.T) {
	var compose struct {
		Services map[string]struct {
			Image    string
			Profiles []string
			Ports    []string
		}
	}
	if err := yaml.Unmarshal([]byte(read(t, "../../compose.yaml")), &compose); err != nil {
		t.Fatal(err)
	}
	candidate, ok := compose.Services["s3-floci"]
	if !ok {
		t.Fatal("the candidate store has no service")
	}
	if !slices.Equal(candidate.Profiles, []string{"floci"}) {
		t.Fatalf("candidate profiles = %v, want only the explicit floci profile", candidate.Profiles)
	}
	if !regexp.MustCompile(`^floci/floci:[^@]+@sha256:[0-9a-f]{64}$`).MatchString(candidate.Image) {
		t.Errorf("candidate image %q needs a version and an immutable digest", candidate.Image)
	}
	if !slices.Equal(candidate.Ports, []string{"${PLATFORMKIT_S3_FLOCI_PORT:-8334}:4566"}) {
		t.Errorf("candidate ports = %v, want its separately overridable endpoint", candidate.Ports)
	}
	makefile := read(t, "../../Makefile")
	for _, goal := range []string{"up", "down"} {
		recipe := regexp.MustCompile(`(?m)^` + goal + `:[^\n]*\n((?:\t[^\n]*(?:\n|$))+)`).FindStringSubmatch(makefile)
		if recipe == nil {
			t.Fatalf("no %s recipe to check", goal)
		}
		namesProfile := strings.Contains(recipe[1], "--profile floci")
		if goal == "up" && namesProfile {
			t.Error("the default stack enables the candidate profile")
		}
		if goal == "down" && !namesProfile {
			t.Error("stack teardown leaves the candidate profile running")
		}
	}
}
