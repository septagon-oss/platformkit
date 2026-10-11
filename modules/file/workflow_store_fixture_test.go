package file_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A job that starts an object store and a suite that dials one are two halves of
// one arrangement, and it holds only while both halves agree on three things:
// which store image, which credentials, which address. The credential question is
// the one a green `make check` on a workstation never asks — the stack `make up`
// started is the stack the Makefile's defaults were written against — and it is
// the one a job can answer wrongly in a way nobody sees until the job runs.
// Measured, not imagined: pointed at a store whose credentials differ from the
// Makefile's defaults, every S3 case in this package answers
//
//	make bucket platformkit-test-82606e5e: Access Denied.
//
// which is as red a gate as no store at all. So this case asks three questions of
// every workflow that runs `make check` and starts a store, and of compose.yaml's
// own s3 service:
//
//   - the store a job starts is the store `make up` starts, digest and all, so a
//     CI run does not test an image nobody develops against;
//   - the credentials it hands that store are the credentials the suite sends,
//     which are PLATFORMKIT_TEST_S3_ACCESS_KEY and ..._SECRET_KEY as the Makefile
//     defaults them;
//   - the address the suite is told matches how the job publishes the store: an
//     endpoint whose host is not loopback has to be the alias the container is
//     given on the job's network, and the loopback default has to be a published
//     port.
//
// Each one is a relation between two files, which is why it is a test and not
// somebody's note: every half of it gets edited on its own.
func TestEveryStoreAJobStartsIsTheStoreTheSuiteDials(t *testing.T) {
	compose := read(t, "../../compose.yaml")
	makefile := read(t, "../../Makefile")

	stackImage := imageRef(t, serviceBlock(t, compose, "s3"))
	accessKey := makeDefault(t, makefile, "PLATFORMKIT_TEST_S3_ACCESS_KEY")
	secretKey := makeDefault(t, makefile, "PLATFORMKIT_TEST_S3_SECRET_KEY")
	endpoint := makeDefault(t, makefile, "PLATFORMKIT_TEST_S3_ENDPOINT")
	published := regexp.MustCompile(`-\s*"?\$\{PLATFORMKIT_S3_PORT:-(\d+)\}:(\d+)"?`).FindStringSubmatch(compose)
	if published == nil {
		t.Fatal("compose.yaml's s3 service publishes no port this case can read")
	}

	// The stack is the first subject: the Makefile's defaults have to be its own
	// credentials, or `make up` and `make check` disagree on a workstation before
	// they ever disagree in CI.
	t.Run("the stack make up starts accepts the credentials the suite sends", func(t *testing.T) {
		block := serviceBlock(t, compose, "s3")
		assertCredentials(t, block, accessKey, secretKey, "compose.yaml's s3 service")
		if published[1] != published[2] {
			t.Errorf("compose.yaml publishes the store's %s on host port %s: the Makefile's default endpoint is %s, so the suite would dial a port no store answers", published[2], published[1], endpoint)
		}
	})

	workflows := filesRunningMakeCheck(t)
	if len(workflows) == 0 {
		t.Fatal("no workflow file runs make check, which is not a fact about this repository")
	}
	started := 0
	for _, path := range sortedPaths(workflows) {
		body := workflows[path]
		if !strings.Contains(body, "seaweedfs") {
			continue
		}
		started++
		t.Run(strings.TrimPrefix(strings.TrimPrefix(path, "../../"), "./"), func(t *testing.T) {
			if got := imageRef(t, body); got != stackImage {
				t.Errorf("starts %s while compose.yaml's s3 service starts %s: the store this job tests would not be the store this module is developed against", got, stackImage)
			}
			assertCredentials(t, body, accessKey, secretKey, path)
			host, port, _ := strings.Cut(endpoint, ":")
			if explicit := regexp.MustCompile(`(?m)^\s*PLATFORMKIT_TEST_S3_ENDPOINT:\s*(\S+)`).FindStringSubmatch(body); explicit != nil {
				host, port, _ = strings.Cut(explicit[1], ":")
			}
			switch host {
			case "localhost", "127.0.0.1":
				if !strings.Contains(body, "-p "+port+":"+port) {
					t.Errorf("leaves the suite at %s but publishes no host port %s for the store it starts", host+":"+port, port)
				}
			default:
				if !strings.Contains(body, "--network-alias "+host) {
					t.Errorf("points the suite at %s:%s but gives the store container no network alias %s: the endpoint names nothing this job can reach", host, port, host)
				}
			}
		})
	}
	if started == 0 {
		t.Fatal("no workflow starts an object store, which TestEveryWorkflowThatRunsMakeCheckNamesAnObjectStore already refuses")
	}
}

// TestTheStoreCasesRefuseToSkipTheStore they drive is the reason the arrangement
// above matters at all: the adapter's cases fail rather than skip without a
// store. Were that ever to flip to a skip, a job could go back to shipping no
// fixture and the suite would stop proving anything about the adapter it ships.
func TestTheStoreCasesRefuseToSkipTheStoreTheyDrive(t *testing.T) {
	source := read(t, "s3_test.go")
	if strings.Contains(source, "t.Skip(") {
		t.Error("s3_test.go skips: these cases were chosen to fail rather than skip without a store, which is what makes the store each CI job starts load-bearing")
	}
	if !strings.Contains(read(t, "internal/s3.go"), "contracts.Storage") {
		t.Fatal("internal/s3.go no longer implements contracts.Storage")
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func sortedPaths(workflows map[string]string) []string {
	paths := make([]string, 0, len(workflows))
	for path := range workflows {
		paths = append(paths, path)
	}
	return paths
}

func filesRunningMakeCheck(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	runsCheck := regexp.MustCompile(`(?m)^\s*run:\s*make\s+check(\s|$)`)
	for _, pattern := range []string{"../../.github/workflows/*.yml", "../../.gitea/workflows/*.yml"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		for _, path := range matches {
			body := read(t, path)
			if runsCheck.FindStringIndex(body) != nil {
				out[path] = body
			}
		}
	}
	return out
}

// serviceBlock is one service's own lines of compose.yaml, so a credential held
// by some other service cannot answer for it.
func serviceBlock(t *testing.T, compose, service string) string {
	t.Helper()
	var lines []string
	inside := false
	for _, line := range strings.Split(compose, "\n") {
		if matched, _ := regexp.MatchString(`^  `+service+`:\s*$`, line); matched {
			inside = true
			continue
		}
		if !inside {
			continue
		}
		if nextService, _ := regexp.MatchString(`^  \S`, line); nextService {
			break
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		t.Fatalf("compose.yaml holds no %s service to read", service)
	}
	return strings.Join(lines, "\n")
}

// imageRef is the pinned reference a file starts its store from. A tag would be
// good enough to answer "which store", but every one of these files pins by
// digest, so the case asks for the digest and finds a missing pin rather than
// passing one along.
func imageRef(t *testing.T, body string) string {
	t.Helper()
	ref := regexp.MustCompile(`chrislusf/seaweedfs@sha256:[0-9a-f]+`).FindString(body)
	if ref == "" {
		t.Fatal("names a SeaweedFS store by no pinned image reference")
	}
	return ref
}

// assertCredentials reads a key in either voice it is written: `-e KEY=value` on
// a docker run line, `KEY: value` in compose's YAML.
func assertCredentials(t *testing.T, body, accessKey, secretKey, where string) {
	t.Helper()
	for _, want := range []struct{ key, value string }{{"AWS_ACCESS_KEY_ID", accessKey}, {"AWS_SECRET_ACCESS_KEY", secretKey}} {
		got := regexp.MustCompile(want.key + `[=:]\s*"?([A-Za-z0-9]+)`).FindStringSubmatch(body)
		if got == nil {
			t.Errorf("%s starts an object store naming no %s: the suite sends %s, and a store that has no such user answers Access Denied, which is as red a gate as no store at all", where, want.key, want.value)
			continue
		}
		if got[1] != want.value {
			t.Errorf("%s gives the store %s=%s while the suite sends %s: every S3 case here would answer Access Denied", where, want.key, got[1], want.value)
		}
	}
}

// makeDefault is a Makefile variable's own default, resolved through its own
// $(...) references, because the endpoint the suite dials is spelled from the
// port the stack publishes: `localhost:$(PLATFORMKIT_S3_PORT)`.
func makeDefault(t *testing.T, makefile, variable string) string {
	t.Helper()
	got := regexp.MustCompile("(?m)^" + variable + `\s*\?=\s*(\S[^\n]*)`).FindStringSubmatch(makefile)
	if got == nil {
		t.Fatalf("Makefile sets no default for %s", variable)
	}
	value := strings.TrimSpace(got[1])
	for {
		open := strings.Index(value, "$(")
		if open < 0 {
			return value
		}
		close := strings.Index(value[open:], ")")
		name := value[open+2 : open+close]
		value = value[:open] + makeDefault(t, makefile, name) + value[open+close+1:]
	}
}
