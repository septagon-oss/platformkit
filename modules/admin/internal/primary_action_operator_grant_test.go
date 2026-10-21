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

func TestAnOperatorPrimaryActionRequiresTheCurrentGrantAndHost(t *testing.T) {
	entry := module.NavEntry{Label: "Manage plans", Screen: "plan/plans", Permission: "plan:write"}
	authorize := grants{}
	p := pages{
		Shell: Shell{Authorize: authorize},
		primary: page.NewNavigation([]module.NavEntry{entry}, []string{"/app/plan/plans"},
			[]tenancy.Grant{{Permission: "plan:write", Operator: true}}),
	}
	tenant := tenancy.Tenant{ID: uuid.New()}
	for _, operator := range []bool{true, false, true} {
		tenant.Operator = operator
		for _, allowed := range []bool{true, false, true} {
			authorize[entry.Permission] = allowed
			body := render(t, g.Group(p.dashboard(t.Context(), tenant, resource.Options{}).Body))
			if got, want := strings.Contains(body, `href="/app/plan/plans"`), operator && allowed; got != want {
				t.Errorf("operator=%v, current grant=%v: primary action present=%v, want %v", operator, allowed, got, want)
			}
		}
	}
}
