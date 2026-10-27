package app

// The composition gate runs at the door every application passes through, not only
// in a validator a caller could forget: a tour that points at a screen this
// composition never published is refused with its routes, before a connection, a
// migration or an event, and a composition whose tours ask nothing is built.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tour"
)

// pointing declares the tour the composition never published a screen for, and
// mounts its one route on the workspace: a composition that mounts nothing there
// is refused as not a product above the declaration gates, so its tours go unasked.
func pointing() module.Module {
	return module.Module{
		Name: "feature",
		Routes: func(s httpx.Surfaces) {
			httpx.Register(s.App, huma.Operation{OperationID: "feature-ping", Method: http.MethodGet, Path: "/ping"},
				httpx.Public(), func(context.Context, *struct{}) (*helloOut, error) { return &helloOut{}, nil })
		},
		Tours: []tour.Tour{{
			Ref:   tour.Ref{ID: "feature.first", Edition: 1},
			Title: tour.Message{Key: "feature.first", Fallback: "Add your first note"},
			Steps: []tour.Step{tour.Spotlight("save", "app/feature/custom/editor#action.submit",
				tour.Content{
					Title: tour.Message{Key: "feature.first.step.save", Fallback: "Save it"},
					Body:  tour.Message{Key: "feature.first.step.save.body", Fallback: "Saving writes the note."}},
				tour.Completes("feature-note-create"), tour.AnchoredOptions{})},
		}},
	}
}

func TestATourPointingPastThisCompositionIsRefusedWithItsRoutes(t *testing.T) {
	cfg, opts := compose(t)
	a, err := New(t.Context(), cfg, []module.Module{pointing()}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = a.Declarations()
	if err == nil {
		t.Fatal("a tour whose screen nobody published built a composition")
	}
	if !RefusedBeforeEffects(err) {
		t.Fatalf("refusal = %v, want one answered before the deployment was touched", err)
	}
	report := err.Error()
	for _, want := range []string{
		"app: invalid tours:",
		`tour "feature.first", step "save": app/feature/custom/editor#action.submit is not an anchor of this composition`,
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not hold %q:\n%s", want, report)
		}
	}
}
