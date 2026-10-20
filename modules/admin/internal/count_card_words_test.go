package internal

// Dashboard counts keep the collection's noun in the destination heading.
// The figure and its noun are one sentence in the reader's own language, and the
// noun a resource declares is the only word it is ever called by: a resource with
// one declared word answers every count with that word (ui/resource's Noun), so
// nothing on the card is a letter the server appended.

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
		nav: page.NewNavigation([]module.NavEntry{{Label: display.Humanize(entity), Screen: "sla/policies"}}, []string{"/app/sla/policies"}, nil),
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
		{"note", 0, "0 Note"},
		{"note", 1, "1 Note"},
		{"note", 3, "3 Note"},
		// A word that is already a set keeps its shape at every count, and so does one
		// that was never declared as a set: nothing here appends a letter to it.
		{"settings", 0, "0 Settings"},
		{"settings", 1, "1 Settings"},
		{"settings", 12, "12 Settings"},
		// Two words in the stored name: the card humanises the same way at 0 and at 2,
		// so the only difference a person meets between them is the number.
		{"sla_policy", 0, "0 Sla policy"},
		{"sla_policy", 1, "1 Sla policy"},
		{"sla_policy", 2, "2 Sla policy"},
	} {
		body := countedDashboard(t, c.entity, c.total)
		if !strings.Contains(body, c.want) {
			t.Errorf("a caller with %d of %q should read %q on the card, and did not:\n%s", c.total, c.entity, c.want, body)
		}
		// The stored name is not a word anybody reads: it is what a card would say if it
		// showed the schema rather than a name. Only a name with an underscore in it is
		// visibly not a word — "note" is a word, and the card's own answer contains it.
		if strings.Contains(c.entity, "_") && strings.Contains(body, c.entity) {
			t.Errorf("a caller with %d of %q is shown the stored name %q:\n%s", c.total, c.entity, c.entity, body)
		}
	}
}
