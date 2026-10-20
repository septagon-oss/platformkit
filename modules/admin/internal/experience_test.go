package internal

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

func TestWorkspaceHomeKeepsReachableWorkBesideCounts(t *testing.T) {
	tenant := tenancy.Tenant{ID: uuid.New(), Name: "Northwind"}
	entries := []module.NavEntry{{Label: "People", Screen: "user/users", Permission: "user:read"}, {Label: "Roles", Screen: "auth/roles", Permission: "role:manage"}}
	p := pages{Shell: Shell{Authorize: allow(true)}, nav: page.NewNavigation(entries, []string{"/app/user/users", "/app/auth/roles"}, nil),
		resources: []httpx.Resource{{Entity: "user", Screen: "/app/user/users", Count: func(_ context.Context) (int64, error) { return 1, nil }}}}
	body := render(t, g.Group(p.dashboard(t.Context(), tenant, resource.Options{}).Body))
	if !strings.Contains(body, `href="/app/auth/roles"`) {
		t.Error("a count hides a reachable workspace")
	}
	if strings.Contains(body, "Every check passes") {
		t.Error("no health evidence was reported as healthy")
	}
	if !strings.Contains(body, "Northwind") {
		t.Error("home never names the tenant whose data is shown")
	}
}

func TestWorkspaceNavigationKeepsTenantAndOperatorScopesDistinct(t *testing.T) {
	a := addresses{workspace: route{at: "/app"}, dashboard: route{at: "/app"}, tenants: route{at: "/app/tenant/tenants"}}
	entries := []module.NavEntry{{Label: "Users", Screen: "user/users", Permission: "user:read"}, {Label: "Tenants", Screen: "tenant/tenants", Permission: "tenant:manage"}}
	nav := page.NewNavigation(entries, []string{"/app/user/users", a.tenants.at}, []tenancy.Grant{{Permission: "tenant:manage", Operator: true}})
	resources := []httpx.Resource{{Screen: "/app/user/users", Present: entity.EntryHints{Icon: "people", Group: &entity.ResourceGroup{Key: "people", Label: "People"}}}}
	for _, operator := range []bool{false, true} {
		r := page.Request{Tenant: tenancy.Tenant{ID: uuid.New(), Name: "Northwind", Operator: operator}, Path: "/app/user/users"}
		visible := nav.Visible(t.Context(), r.Tenant, allow(true))
		props := sidebar(a, visible, r, false, resources...)
		for _, node := range []g.Node{components.Sidebar(props), components.SidebarDisclosure(props)} {
			body := render(t, node)
			if strings.Contains(body, a.tenants.at) != operator {
				t.Errorf("operator=%v: cross-tenant link disagrees with scope", operator)
			}
			if !strings.Contains(body, `aria-current="page"`) || !strings.Contains(body, "/app/user/users") {
				t.Error("navigation lost the current authorized page")
			}
		}
	}
}

func TestWorkspaceCountsNeverReadAnUnauthorizedDestination(t *testing.T) {
	called := false
	p := pages{Shell: Shell{Authorize: allow(false)}, nav: page.NewNavigation([]module.NavEntry{{Label: "Users", Screen: "user/users", Permission: "user:read"}}, []string{"/app/user/users"}, nil), resources: []httpx.Resource{{Screen: "/app/user/users", Count: func(context.Context) (int64, error) { called = true; return 42, nil }}}}
	body := render(t, g.Group(p.dashboard(t.Context(), tenancy.Tenant{ID: uuid.New()}, resource.Options{}).Body))
	if called || strings.Contains(body, "/app/user/users") {
		t.Fatal("home read or linked a destination this role cannot open")
	}
}
