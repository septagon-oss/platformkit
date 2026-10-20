package internal

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

// TestHealthThatWasNeverMeasuredIsNotReportedAsPassing: with no database on
// the request no check runs, so neither the operator's home nor the health page
// may wear a success tone.
func TestHealthThatWasNeverMeasuredIsNotReportedAsPassing(t *testing.T) {
	p := pages{Shell: Shell{Authorize: allow(true)}, nav: page.NewNavigation(nil, nil, nil)}
	operator := tenancy.Tenant{ID: uuid.New(), Name: "PlatformKit", Operator: true}
	view := p.dashboard(t.Context(), operator, resource.Options{})
	home := render(t, g.Group(view.Body))
	if view.Title == "" || !strings.Contains(home, "<h1") {
		t.Fatalf("the operator's home did not render: %s", home)
	}
	if strings.Contains(home, `data-tone="success"`) {
		t.Errorf("the operator's home reports health it never measured: %s", home)
	}
	health := render(t, g.Group(healthPage(checks(t.Context()), resource.Options{}).Body))
	if strings.Contains(health, `data-tone="success"`) {
		t.Errorf("the health page reports a check that never ran as passing: %s", health)
	}
}
