package designconfig_test

import (
	"go/build"
	"slices"
	"strings"
	"testing"
)

// The loader is the one package allowed to decode YAML into the values design
// owns, so it reaches nothing past design and the decoder: design stays
// standard-library-only and kit/config stays runtime configuration.
func TestTheLoaderImportsOnlyDesignAndYAML(t *testing.T) {
	allowed := []string{"github.com/septagon-oss/platformkit/design", "gopkg.in/yaml.v3"}
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("read the package's own sources: %v", err)
	}
	if len(pkg.GoFiles) == 0 {
		t.Fatal("the package declares no production source to measure")
	}
	for _, imp := range pkg.Imports {
		standard := !strings.Contains(strings.SplitN(imp, "/", 2)[0], ".")
		if !standard && !slices.Contains(allowed, imp) {
			t.Errorf("production source imports %q; the loader may reach only the standard library and %v", imp, allowed)
		}
	}
}
