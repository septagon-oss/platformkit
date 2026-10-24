package task_test

import (
	"os"
	"strings"
	"testing"
)

// The module's declaration is what a composition plans from, so it must name
// every contract the wiring puts: Service, Creator, Reader and LockedReader.
func TestModuleDeclaresEachContractItPuts(t *testing.T) {
	src, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"Service", "Creator", "Reader", "LockedReader"} {
		for _, form := range []string{"pkit.Provides[taskcontracts." + c + "]()", "pkit.Put[taskcontracts." + c + "]("} {
			if !strings.Contains(string(src), form) {
				t.Errorf("provider.go lacks %s", form)
			}
		}
	}
}
