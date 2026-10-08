package telemetry_test

// The pin of the one dependency rule this delivery is built on.
//
// `kit/telemetry/README.md` states it as a gate rather than as a hope: "Those are
// chosen once, by the composition, in `kit/app`, which is the only package in this
// repository whose dependency closure may hold an OpenTelemetry SDK;
// `scripts/check_packages.sh` refuses the day that stops being true."
//
// That script measures the transitive closure of a fixed list of packages
// (`parts`, at the top of the script) and refuses a dependency outside what each of
// those may hold — `measurement`, the SDK/exporter/gRPC list, is admitted only for
// `kit/app`. A package that is not in that list is not measured: a module's
// `internal` package that imported an exporter would be counted by nothing there,
// and the compiler has no opinion about it either. This case asks the question of
// every non-test Go file in the repository, which is the sentence the guide prints.
//
// The scope is the script's own: test files are excluded, because the doubles this
// delivery tests with are the SDK's `tracetest.NewSpanRecorder` and
// `sdkmetric.NewManualReader`, and the guide names them as the reason.
//
// The case pins a rule this branch wrote, not a number: it passes while the rule
// holds and fails on the day somebody installs a second provider — which is the day
// the guide says the gate refuses. It cannot be satisfied by moving a figure, and it
// says nothing about anybody else's branch.

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// sdkImportPrefixes are the import paths that mean "this file can install a
// provider or reach a collector", as opposed to the API — otel, otel/trace,
// otel/metric and otelhttp — which is what every other kernel package imports and
// which drags no provider, no exporter and no transport with it.
var sdkImportPrefixes = []string{
	"go.opentelemetry.io/otel/sdk",
	"go.opentelemetry.io/otel/exporters",
}

func TestOnlyTheCompositionLinksTheMeasurementSDK(t *testing.T) {
	root := filepath.Join("..", "..")
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "kit/app/") {
			return nil // the composition is the one package the rule names
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			for _, prefix := range sdkImportPrefixes {
				if path == prefix || strings.HasPrefix(path, prefix+"/") {
					offenders = append(offenders, rel+" imports "+path)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository's Go files: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("kit/app is not the only package that links the OpenTelemetry SDK or an exporter, "+
			"which is what kit/telemetry/README.md says scripts/check_packages.sh refuses, and what keeps the "+
			"process to one TracerProvider and one MeterProvider:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}
