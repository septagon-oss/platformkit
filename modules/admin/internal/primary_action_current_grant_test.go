package internal

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestTheHomeRechecksThePrimaryActionAfterItsGrantIsRevoked(t *testing.T) {
	authorize := grants{"task:read": true}
	p := pages{Shell: Shell{Authorize: authorize}, primary: primaryOf(goToTasks)}
	tenant := tenancy.Tenant{ID: uuid.New()}
	for _, allowed := range []bool{true, false, true} {
		authorize["task:read"] = allowed
		body := render(t, g.Group(p.dashboard(t.Context(), tenant, resource.Options{}).Body))
		if got := strings.Contains(body, `href="/app/task/tasks"`); got != allowed {
			t.Fatalf("current task:read=%v: primary destination present=%v", allowed, got)
		}
	}
}

func TestThePrimaryActionKeepsOperatorDestinationsOffCustomerHosts(t *testing.T) {
	entry := module.NavEntry{Label: "Manage plans", Screen: "plan/plans", Permission: "plan:write"}
	p := pages{
		Shell: Shell{Authorize: allow(true)},
		primary: page.NewNavigation([]module.NavEntry{entry}, []string{"/app/plan/plans"},
			[]tenancy.Grant{{Permission: "plan:write", Operator: true}}),
	}
	for _, operator := range []bool{false, true, false} {
		tenant := tenancy.Tenant{ID: uuid.New(), Operator: operator}
		body := render(t, g.Group(p.dashboard(t.Context(), tenant, resource.Options{}).Body))
		if got := strings.Contains(body, `href="/app/plan/plans"`); got != operator {
			t.Fatalf("operator host=%v: primary destination present=%v", operator, got)
		}
	}
}
