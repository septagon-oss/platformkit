package pkit_test

import (
	"errors"
	"testing"

	engine "github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestEmptyNameRefusalsKeepTheirDocumentedErrorTypes(t *testing.T) {
	a := pkit.NewApp("")
	_, explainErr := a.Explain(dev)
	_, describeErr := a.Describe(dev)
	plan, planErr := a.Plan(dev)
	if plan != nil {
		t.Fatal("an empty app name returned a plan")
	}
	runtime, buildErr := a.Build(t.Context(), dev)
	if runtime != nil {
		defer runtime.Close()
		t.Fatal("an empty app name returned a runtime")
	}
	for _, tc := range []struct {
		entry      string
		err        error
		structured bool
	}{
		{"Validate", a.Validate(dev), true},
		{"Explain", explainErr, true},
		{"Describe", describeErr, true},
		{"Plan", planErr, false},
		{"Build", buildErr, false},
		{"Run", a.Run(t.Context(), dev, engine.All), false},
	} {
		t.Run(tc.entry, func(t *testing.T) {
			if tc.err == nil {
				t.Fatal("an empty app name was accepted")
			}
			if _, ok := errors.AsType[*pkit.CompositionError](tc.err); ok != tc.structured {
				t.Errorf("CompositionError = %v, want %v; error: %v", ok, tc.structured, tc.err)
			}
		})
	}
}
