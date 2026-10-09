package internal

// count_card_words_test.go pins the words a dashboard card counts with. The card
// is the one place the shell turns an entity's stored name into a sentence, and it
// writes that sentence at three counts: none, one, and however many there are. The
// three must agree on the noun — a person who read "No sla_policys yet" and came
// back to a page reading "2 Sla policys" would conclude the empty grid and the full
// one were about different things — so the cases below include an entity whose
// stored name is two words, which is the shape that makes the humanising visible.
//
// No database and no browser: the count is the resource's own function, and with no
// connection on the context the health check answers nothing (see checks), which
// leaves the card as the only variable in the page.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// countedDashboard renders the dashboard for one resource holding total.
func countedDashboard(t *testing.T, entity string, total int64) string {
	t.Helper()
	p := pages{resources: []httpx.Resource{{
		Module: "sla", Entity: entity, Screen: "/app/sla/policies",
		Count: func(context.Context) (int64, error) { return total, nil },
	}}}
	return render(t, g.Group(p.dashboard(context.Background(), tenancy.Tenant{ID: uuid.New()}).Body))
}

func TestTheDashboardCountsTheSameNounAtEveryCount(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		entity string
		total  int64
		want   string
	}{
		{"note", 0, "No notes yet"},
		{"note", 1, "1 Note"},
		{"note", 3, "3 Notes"},
		// A mass noun asks no plural at any count, and "1 Settings" is the oddity the
		// rule accepts rather than invents a singular for.
		{"settings", 0, "No settings yet"},
		{"settings", 1, "1 Settings"},
		{"settings", 12, "12 Settings"},
		// Two words in the stored name: the zero card humanises the way the other two
		// do, so the only difference a person meets between 0 and 2 is the number.
		{"sla_policy", 0, "No sla policys yet"},
		{"sla_policy", 1, "1 Sla policy"},
		{"sla_policy", 2, "2 Sla policys"},
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
