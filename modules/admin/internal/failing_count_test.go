package internal

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

// TestAFailingCountKeepsTheDestinationReachable: a destination the caller may
// open stays on the home page when its counter fails, and the failure is not
// drawn as a number.
func TestAFailingCountKeepsTheDestinationReachable(t *testing.T) {
	called := false
	p := pages{Shell: Shell{Authorize: allow(true)},
		nav: page.NewNavigation([]module.NavEntry{{Label: "Users", Screen: "user/users", Permission: "user:read"}}, []string{"/app/user/users"}, nil),
		resources: []httpx.Resource{{Entity: "user", Screen: "/app/user/users", Count: func(context.Context) (int64, error) {
			called = true
			return 0, errors.New("the counter is down")
		}}}}
	body := render(t, g.Group(p.dashboard(t.Context(), tenancy.Tenant{ID: uuid.New(), Name: "Northwind"}, resource.Options{}).Body))
	if !called {
		t.Fatal("home never asked the authorized destination's counter")
	}
	card := regexp.MustCompile(`(?s)<a [^>]*href="/app/user/users"[^>]*>.*?</a>`).FindString(body)
	if card == "" || !strings.Contains(card, `data-component="card"`) {
		t.Fatalf("a failing counter took the authorized destination off home:\n%s", body)
	}
	if strings.Contains(card, `data-component="badge"`) || strings.Contains(card, "the counter is down") {
		t.Errorf("a failed count is drawn as a figure or an error on the destination: %s", card)
	}
}
