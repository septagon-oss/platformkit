package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, root, name, text string) {
	t.Helper()
	file := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func jsonStream(t *testing.T, records any) []byte {
	t.Helper()
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fakeCommand(t *testing.T, root string, packages []byte) command {
	t.Helper()
	return func(_ context.Context, directory string, args ...string) ([]byte, error) {
		if directory != root {
			t.Errorf("directory = %s, want %s", directory, root)
		}
		switch strings.Join(args, " ") {
		case "env GOFLAGS":
			return nil, nil
		case "list -m -json":
			return jsonStream(t, module{Path: product, Dir: root, Main: true}), nil
		case "list -f {{.ImportPath}} std":
			return []byte("context\nfmt\ntesting\n"), nil
		case "list -json -test ./...":
			return packages, nil
		default:
			t.Fatalf("unexpected command: %q", args)
			return nil, nil
		}
	}
}

func TestCommandModesAndOutputFailures(t *testing.T) {
	root := t.TempDir()
	p := packageFixture("modules/a")
	p.Dir, p.Module.Dir = filepath.Join(root, "modules/a"), root
	p.Imports = []string{product + "/modules/b"}
	for _, measure := range []bool{false, true} {
		args, want := []string{root}, 1
		if measure {
			args, want = []string{"--edges", root}, 0
		}
		var stdout, stderr bytes.Buffer
		status := run(t.Context(), args, &stdout, &stderr, fakeCommand(t, root, jsonStream(t, p)))
		if status != want || !strings.Contains(stderr.String(), "foreign-implementation") || strings.Contains(stdout.String(), "satisfy") {
			t.Fatalf("status %d, stdout %s, stderr %s", status, &stdout, &stderr)
		}
		if measure {
			var report report
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || len(report.Pairs) != 1 || len(report.Pairs[0].Edges) != 1 {
				t.Fatalf("invalid report: %v; %s", err, &stdout)
			}
		}
	}
	for _, args := range [][]string{{root}, {"--edges", root}} {
		var output bytes.Buffer
		if code := run(t.Context(), args, &output, &output, fakeCommand(t, root, nil)); code != 0 {
			t.Fatalf("empty graph: %d, %s", code, &output)
		}
		if code := run(t.Context(), args, brokenWriter{}, &output, fakeCommand(t, root, nil)); code != 2 {
			t.Fatalf("write failure returned %d", code)
		}
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("output is closed") }

func TestFailedAcquisitionCannotProduceSuccess(t *testing.T) {
	root := t.TempDir()
	p := packageFixture("modules/a")
	p.Dir, p.Module.Dir = filepath.Join(root, "modules/a"), root
	valid := jsonStream(t, p)
	for _, fault := range []string{"partial process", "truncated json", "null identity", "standard command", "empty standard", "main metadata", "module write mode"} {
		t.Run(fault, func(t *testing.T) {
			base := fakeCommand(t, root, valid)
			call := func(ctx context.Context, dir string, args ...string) ([]byte, error) {
				key := strings.Join(args, " ")
				switch {
				case fault == "partial process" && key == "list -json -test ./...":
					return valid, errors.New("go list: importer imports forbidden/internal: not allowed")
				case fault == "truncated json" && key == "list -json -test ./...":
					return append(valid, []byte(`{"ImportPath":`)...), nil
				case fault == "null identity" && key == "list -json -test ./...":
					return []byte("null"), nil
				case fault == "standard command" && strings.HasSuffix(key, " std"):
					return nil, errors.New("standard load failed")
				case fault == "empty standard" && strings.HasSuffix(key, " std"):
					return nil, nil
				case fault == "main metadata" && key == "list -m -json":
					return []byte(`{"Path":"example.test/product","Main":true}`), nil
				case fault == "module write mode" && key == "env GOFLAGS":
					return []byte("-tags=custom -mod=mod"), nil
				}
				return base(ctx, dir, args...)
			}
			for _, args := range [][]string{{root}, {"--edges", root}} {
				var stdout, stderr bytes.Buffer
				if code := run(t.Context(), args, &stdout, &stderr, call); code != 2 || stdout.Len() != 0 {
					t.Fatalf("got %d, stdout %s, stderr %s", code, &stdout, &stderr)
				}
			}
		})
	}
}

func TestRealGraphUsesGoSelectionAndTestProvenance(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module "+product+"\n\ngo 1.26.6\n")
	writeFixture(t, root, "scripts/imports-layers.txt", product+"/modules/a platform\n"+product+"/modules/b platform\n")
	writeFixture(t, root, "modules/a/a.go", "package a\n// import \""+product+"/modules/b\"\nconst Prose = `\""+product+"/modules/b\"`\n")
	writeFixture(t, root, "modules/b/b.go", "package b\n")
	writeFixture(t, root, "modules/b/contracts/btest/helper.go", "package btest\nimport _ \""+product+"/modules/a\"\n")
	writeFixture(t, root, "modules/a/a_test.go", "package a_test\nimport _ \""+product+"/modules/b/contracts/btest\"\n")
	writeFixture(t, root, "modules/a/ignored.go", "//go:build custom\n\npackage a\nimport _ `"+product+"/modules/b`\n")
	// The helper's source imports a constructor. It is an ordinary package,
	// even when Go also rebuilds it for the external test binary.
	args := []string{root}
	check := func(want int, rule string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := run(t.Context(), args, &stdout, &stderr, runGo)
		if code != want || !strings.Contains(stderr.String(), rule) {
			t.Fatalf("status %d want %d, stdout %s, stderr %s", code, want, &stdout, &stderr)
		}
	}
	check(1, "foreign-implementation")
	writeFixture(t, root, "modules/b/contracts/btest/helper.go", "package btest\n")
	check(0, "")
	t.Setenv("GOFLAGS", "-buildvcs=false -tags=custom")
	check(1, "foreign-implementation")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	writeFixture(t, root, "modules/a/untracked.go", "package a\nimport _ `"+product+"/modules/b/contracts/btest`\n")
	check(1, "test-helper-in-production")
	writeFixture(t, root, "modules/a/untracked.go", "package a\n")
	writeFixture(t, root, "modules/a/a_test.go", "package a_test\nimport _ \""+product+"/modules/b\"\n")
	check(1, "undeclared-integration")
	writeFixture(t, root, "scripts/imports-integration.txt", "modules/a@external "+product+"/modules/b Composes the public constructor.\n")
	check(0, "integration modules/a@external; used")
	writeFixture(t, root, "modules/b/internal/store.go", "package internal\n")
	writeFixture(t, root, "modules/a/untracked.go", "package a\nimport _ \""+product+"/modules/b/internal\"\n")
	check(2, "use of internal package "+product+"/modules/b/internal not allowed")
	external := t.TempDir()
	writeFixture(t, external, "go.mod", fmt.Sprintf("module %s\n\ngo 1.26.6\nrequire %s v0.0.0\nreplace %s => %q\n", dependency, product, product, root))
	writeFixture(t, external, "scripts/imports-layers.txt", dependency+"/modules/b platform\n")
	writeFixture(t, external, "modules/b/contracts/values.go", "package contracts\nimport _ \""+product+"/modules/a\"\n")
	writeFixture(t, root, "go.mod", fmt.Sprintf("module %s\n\ngo 1.26.6\nrequire %s v0.0.0\nreplace %s => %q\n", product, dependency, dependency, external))
	writeFixture(t, root, "modules/a/untracked.go", "package a\n")
	writeFixture(t, root, "modules/a/local_test.go", "package a\n")
	writeFixture(t, root, "modules/a/a_test.go", "package a_test\nimport _ \""+dependency+"/modules/b/contracts\"\n")
	args = append(args, dependency)
	check(0, "")
}

func TestGoProcessRetainsItsDiagnostic(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go", "#!/bin/sh\nprintf '{}\\n'\necho 'importer imports target/internal: denied' >&2\nexit 73\n")
	if err := os.Chmod(filepath.Join(root, "go"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := runGo(t.Context(), root, "list", "-json", "-test", "./...")
	if err == nil || !strings.Contains(err.Error(), "importer imports target/internal") || !strings.Contains(err.Error(), root) || !strings.Contains(err.Error(), "exit status 73") {
		t.Fatalf("diagnostic = %v", err)
	}
}

func TestDeclaredDependencyIsResolvedAndDeduplicated(t *testing.T) {
	root := t.TempDir()
	p := packageFixture("modules/a")
	p.Dir, p.Module.Dir = filepath.Join(root, "modules/a"), root
	p.Imports = []string{dependency + "/modules/a/contracts"}
	base := fakeCommand(t, root, jsonStream(t, p))
	dependencyDir := filepath.Join(root, "dependency")
	writeFixture(t, dependencyDir, "scripts/imports-layers.txt", dependency+"/modules/a platform\n")
	writeFixture(t, root, "scripts/imports-layers.txt", product+"/modules/a platform\n")
	queries := 0
	call := func(ctx context.Context, root string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "list -m -json "+dependency {
			queries++
			return jsonStream(t, module{Path: dependency, Dir: dependencyDir}), nil
		}
		return base(ctx, root, args...)
	}
	var output bytes.Buffer
	if code := run(t.Context(), []string{root, product, dependency, dependency}, &output, &output, call); code != 0 || queries != 1 {
		t.Fatalf("code %d, queries %d, output %s", code, queries, &output)
	}
	for _, invalid := range []string{"../dependency", "example.test/foundation@v1", "-bad", "example.test/*"} {
		if code := run(t.Context(), []string{root, invalid}, &output, &output, call); code != 2 {
			t.Fatalf("invalid root %q returned %d", invalid, code)
		}
	}
	unresolved := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("module unavailable") }
	if code := run(t.Context(), []string{root, dependency}, &output, &output, unresolved); code != 2 {
		t.Fatalf("unresolved module returned %d", code)
	}
}

func TestMalformedPolicyRefusesBothModes(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "scripts/imports-integration.txt", "modules/a@external "+product+"/modules/b\n")
	for _, args := range [][]string{{root}, {"--edges", root}} {
		var stdout, stderr bytes.Buffer
		if code := run(t.Context(), args, &stdout, &stderr, fakeCommand(t, root, nil)); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "imports-integration.txt:1") {
			t.Fatalf("code %d, stdout %s, stderr %s", code, &stdout, &stderr)
		}
	}
}

func TestTransitiveMetadataAddsNoDirectEdge(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "scripts/imports-layers.txt", product+"/modules/a platform\n"+product+"/modules/b platform\n"+product+"/modules/c platform\n")
	p := packageFixture("modules/a")
	p.Dir, p.Module.Dir = filepath.Join(root, "modules/a"), root
	p.Imports = []string{product + "/modules/b/contracts"}
	data := jsonStream(t, p)
	data = append(data[:len(data)-1], []byte(`,"Deps":["`+product+`/modules/c/internal"]}`)...)
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{"--edges", root}, &stdout, &stderr, fakeCommand(t, root, data)); code != 0 || strings.Contains(stdout.String(), "/modules/c") || strings.Contains(stderr.String(), "OUT OF BOUNDS") {
		t.Fatalf("code %d, stdout %s, stderr %s", code, &stdout, &stderr)
	}
}

func TestWrapperPreservesTargetContextAndStatus(t *testing.T) {
	root := t.TempDir()
	script, err := os.ReadFile("../../scripts/check_imports.sh")
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "foundation/scripts/check_imports.sh", string(script))
	writeFixture(t, root, "bin/go", `#!/bin/sh
case "$1" in
env) echo host ;;
build)
    [ "$GOWORK" = off ] && [ -z "$GOFLAGS" ] || exit 99
    while [ "$1" != -o ]; do shift; done
    shift
    cat > "$1" <<'PROGRAM'
#!/bin/sh
printf '%s\n' "$@" "$GOFLAGS" "$GOWORK"
exit "$FIXTURE_STATUS"
PROGRAM
    chmod +x "$1"
    ;;
esac
`)
	if err := os.Chmod(filepath.Join(root, "bin/go"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOFLAGS", "-tags=selected")
	t.Setenv("GOWORK", "original-workspace")
	target := filepath.Join(root, "target with spaces")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{0, 1, 2} {
		t.Setenv("FIXTURE_STATUS", fmt.Sprint(status))
		cmd := exec.CommandContext(t.Context(), "bash", filepath.Join(root, "foundation/scripts/check_imports.sh"), "target with spaces", dependency)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		code := 0
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		want := target + "\n" + dependency + "\n-tags=selected\noriginal-workspace\n"
		if code != status || string(output) != want {
			t.Fatalf("status %d, output %q, want %d %q", code, output, status, want)
		}
	}
	t.Setenv("FIXTURE_STATUS", "0")
	cmd := exec.CommandContext(t.Context(), "bash", filepath.Join(root, "foundation/scripts/check_imports.sh"))
	cmd.Dir = target
	if output, err := cmd.CombinedOutput(); err != nil || !strings.HasPrefix(string(output), filepath.Join(root, "foundation")+"\n") {
		t.Fatalf("default foundation: %s; %v", output, err)
	}
}

// TestModuleGraphModeNamesOneReportAndOutlivesARefusal checks the aggregate report
// through the command: the two report modes describe different graphs, so naming both
// is a configuration error, and the module report is a measurement rather than a
// verdict — scripts/module_graph.sh renders it over a tree the policy refuses, and a
// module the build holds with no row stays listed as unassigned.
func TestModuleGraphModeNamesOneReportAndOutlivesARefusal(t *testing.T) {
	root := t.TempDir()
	p := packageFixture("modules/a")
	p.Dir, p.Module.Dir = filepath.Join(root, "modules/a"), root
	p.Imports = []string{product + "/modules/b"}
	stream := jsonStream(t, p)
	var both bytes.Buffer
	if status := run(t.Context(), []string{"--edges", "--modules", root}, &both, &both, fakeCommand(t, root, stream)); status != 2 ||
		!strings.Contains(both.String(), "name one") {
		t.Fatalf("two reports named together returned status %d: %s", status, &both)
	}
	var stdout, stderr bytes.Buffer
	if status := run(t.Context(), []string{"--modules", root}, &stdout, &stderr, fakeCommand(t, root, stream)); status != 0 ||
		!strings.Contains(stderr.String(), "module-layer-missing") || strings.Contains(stdout.String(), "satisfy") {
		t.Fatalf("status %d, stdout %s, stderr %s", status, &stdout, &stderr)
	}
	var graph moduleReport
	if err := json.Unmarshal(stdout.Bytes(), &graph); err != nil || len(graph.Nodes) != 2 {
		t.Fatalf("invalid module report: %v; %s", err, &stdout)
	}
	for _, node := range graph.Nodes {
		if node.Layer != layerUnassigned {
			t.Errorf("module %s is reported in layer %s with no row naming it", node.Module, node.Layer)
		}
	}
}
