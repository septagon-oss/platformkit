package internal

// Dashboard counts keep the collection's noun in the destination heading.
// An empty collection says so in words; positive counts keep their numeric badge.

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// countedDashboard renders the dashboard for one resource holding total.
func countedDashboard(t *testing.T, entity string, total int64) string {
	t.Helper()
	p := pages{Shell: Shell{Authorize: allow(true)},
		nav: page.NewNavigation([]module.NavEntry{{Label: display.Plural(display.Humanize(entity)), Screen: "sla/policies"}}, []string{"/app/sla/policies"}, nil),
		resources: []httpx.Resource{{
			Module: "sla", Entity: entity, Screen: "/app/sla/policies",
			Count: func(context.Context) (int64, error) { return total, nil },
		}}}
	body := render(t, g.Group(p.dashboard(t.Context(), tenancy.Tenant{ID: uuid.New()}, resource.Options{}).Body))
	return strings.Join(strings.Fields(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(body, " ")), " ")
}

func TestTheDashboardCountsTheSameNounAtEveryCount(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		entity string
		total  int64
		want   string
	}{
		{"note", 0, "No notes yet"},
		{"note", 1, "Notes 1"},
		{"note", 3, "Notes 3"},
		// A mass noun keeps its shape at every count.
		{"settings", 0, "No settings yet"},
		{"settings", 1, "Settings 1"},
		{"settings", 12, "Settings 12"},
		// Two words in the stored name: the zero card humanises the way the other two
		// do, so the only difference a person meets between 0 and 2 is the number.
		{"sla_policy", 0, "No sla policys yet"},
		{"sla_policy", 1, "Sla policys 1"},
		{"sla_policy", 2, "Sla policys 2"},
	} {
		body := countedDashboard(t, c.entity, c.total)
		if !strings.Contains(body, c.want) {
			t.Errorf("a caller with %d of %q should read %q on the card, and did not:\n%s", c.total, c.entity, c.want, body)
		}
		// The stored name is not a word anybody reads, and it is what the card falls back
		// to when the zero case lowers before it humanises: "No sla_policys yet". Only a
		// name with an underscore in it is visibly not a word — "note" is a word, and its
		// plural contains it.
		if strings.Contains(c.entity, "_") && strings.Contains(body, c.entity) {
			t.Errorf("a caller with %d of %q is shown the stored name %q:\n%s", c.total, c.entity, c.entity, body)
		}
	}
}
