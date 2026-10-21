package internal

import (
	"os"
	"strings"
	"testing"
)

// Every rest.Spec the module mounts declares its names, so composition does
// not refuse it.
func TestEveryDecideSpecDeclaresItsNames(t *testing.T) {
	src, err := os.ReadFile("handler.go")
	if err != nil {
		t.Fatal(err)
	}
	specs := strings.Count(string(src), ":= rest.Spec[")
	named := strings.Count(string(src), "]{Name: rest.Name{One:")
	if specs == 0 || named < specs {
		t.Fatalf("%d specs, %d with a declared name", specs, named)
	}
}
