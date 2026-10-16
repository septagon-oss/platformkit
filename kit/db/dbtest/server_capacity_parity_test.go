package dbtest_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// maxConnections reads the server limit a Postgres service sets on its command
// line or through initdb, and is empty when the service leaves the image default.
var maxConnections = regexp.MustCompile(`max_connections=(\d+)`)

type postgresService struct {
	Image   string            `yaml:"image"`
	Command []string          `yaml:"command"`
	Env     map[string]string `yaml:"env"`
}

// The suite's pool bounds are priced against one server limit, so every server
// the suite runs against — the workstation's compose stack and each CI service
// container — must name that limit, and name the same one.
func TestEveryPostgresServiceAllowsTheSameConnectionCount(t *testing.T) {
	limits := map[string]string{}

	var compose struct {
		Services map[string]postgresService `yaml:"services"`
	}
	readYAML(t, "../../../compose.yaml", &compose)
	for name, service := range compose.Services {
		if strings.HasPrefix(service.Image, "postgres") {
			limits["compose.yaml "+name] = limitOf(strings.Join(service.Command, " "))
		}
	}

	var workflows []string
	for _, pattern := range []string{"../../../.github/workflows/*.yml", "../../../.gitea/workflows/*.yml"} {
		found, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		workflows = append(workflows, found...)
	}
	for _, path := range workflows {
		var workflow struct {
			Jobs map[string]struct {
				Services map[string]postgresService `yaml:"services"`
			} `yaml:"jobs"`
		}
		readYAML(t, path, &workflow)
		for job, spec := range workflow.Jobs {
			for name, service := range spec.Services {
				if strings.HasPrefix(service.Image, "postgres") {
					limits[path+" "+job+"."+name] = limitOf(service.Env["POSTGRES_INITDB_ARGS"])
				}
			}
		}
	}

	if len(limits) < 2 {
		t.Fatalf("found %d Postgres service(s), want compose.yaml's and at least one workflow's: %v", len(limits), limits)
	}
	want := limits["compose.yaml postgres"]
	if want == "" {
		t.Fatalf("compose.yaml's postgres service sets no max_connections: %v", limits)
	}
	for where, got := range limits {
		if got != want {
			t.Errorf("%s allows max_connections=%q, compose.yaml allows %q", where, got, want)
		}
	}
}

func limitOf(args string) string {
	if m := maxConnections.FindStringSubmatch(args); m != nil {
		return m[1]
	}
	return ""
}

func readYAML(t *testing.T, path string, into any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := yaml.Unmarshal(raw, into); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}
