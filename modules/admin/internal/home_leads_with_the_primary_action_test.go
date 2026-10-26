package internal

// home_leads_with_the_primary_action_test.go is the order of the workspace home:
// the application's primary action first, for whoever may follow it, and the
// counters after it, each only for whoever may read what it counts. No database:
// the home renders from the navigation values and the resources' Count doubles.

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// grants is an Authorizer that answers by permission, where allow answers every
// grant alike.
type grants map[string]bool

func (g grants) Allowed(_ context.Context, _ tenancy.Tenant, grant tenancy.Grant) (bool, error) {
	return g[grant.Permission], nil
}

// goToTasks is the reference product's declaration, as the shell holds it.
var goToTasks = module.NavEntry{Label: "Go to tasks", Screen: "task/tasks", Permission: "task:read"}

// homeWith is the home for one authorizer, with People and Roles in the
// navigation, a Count on People that reports through counted, and the given
// primary navigation.
func homeWith(t *testing.T, authorize httpx.Authorizer, primary page.Navigation, counted *bool) string {
	t.Helper()
	entries := []module.NavEntry{{Label: "People", Screen: "user/users", Permission: "user:read"}, {Label: "Roles", Screen: "auth/roles", Permission: "role:manage"}}
	p := pages{Shell: Shell{Authorize: authorize}, nav: page.NewNavigation(entries, []string{"/app/user/users", "/app/auth/roles"}, nil),
		primary: primary,
		resources: []httpx.Resource{{Entity: "user", Screen: "/app/user/users", Count: func(context.Context) (int64, error) {
			if counted != nil {
				*counted = true
			}
			return 1, nil
		}}}}
	return render(t, g.Group(p.dashboard(t.Context(), tenancy.Tenant{ID: uuid.New(), Name: "Northwind"}, resource.Options{}).Body))
}

func primaryOf(entry module.NavEntry) page.Navigation {
	return page.NewNavigation([]module.NavEntry{entry}, []string{httpx.Workspace(entry.Screen)}, nil)
}

var primaryButton = regexp.MustCompile(`<a [^>]*data-component="button"[^>]*data-variant="primary"[^>]*>`)

func TestTheHomeLeadsWithThePrimaryAction(t *testing.T) {
	body := homeWith(t, allow(true), primaryOf(goToTasks), nil)
	buttons := primaryButton.FindAllStringIndex(body, -1)
	if len(buttons) != 1 {
		t.Fatalf("the home draws %d primary buttons, want exactly one:\n%s", len(buttons), body)
	}
	action := buttons[0][0]
	tag := body[buttons[0][0]:buttons[0][1]]
	if !strings.Contains(tag, `href="/app/task/tasks"`) || !strings.Contains(body[action:], "Go to tasks<") {
		t.Errorf("the primary button is %s, want the action to /app/task/tasks labelled %q", tag, goToTasks.Label)
	}
	if h1 := strings.Index(body, "<h1"); h1 < 0 || h1 > action {
		t.Errorf("the action stands before the page's title (h1 at %d, action at %d)", h1, action)
	}
	for _, after := range []string{"Manage your team and the work happening here.", `data-component="card"`, `data-component="badge"`} {
		if i := strings.Index(body, after); i < 0 || i < action {
			t.Errorf("%q comes at %d, before the action at %d (or not at all)", after, i, action)
		}
	}
}

func TestThePrimaryActionIsDrawnOnlyForWhoMayFollowIt(t *testing.T) {
	body := homeWith(t, grants{"user:read": true}, primaryOf(goToTasks), nil)
	if strings.Contains(body, `href="/app/task/tasks"`) {
		t.Errorf("the action is drawn for a caller who may not read tasks:\n%s", body)
	}
	if !strings.Contains(body, `href="/app/user/users"`) || !strings.Contains(body, `data-component="badge"`) {
		t.Errorf("the rest of the home is gone with the action:\n%s", body)
	}
}

func TestAHomeWithNoPrimaryActionIsTheDashboardItWas(t *testing.T) {
	none := homeWith(t, allow(true), page.Navigation{}, nil)
	if primaryButton.MatchString(none) {
		t.Errorf("a home with no primary action draws a primary button:\n%s", none)
	}
	// An action no route serves is reported at boot and draws nothing.
	unserved := homeWith(t, allow(true), page.NewNavigation([]module.NavEntry{goToTasks}, nil, nil), nil)
	if unserved != none {
		t.Errorf("an unserved action changed the home:\n%s\nwant:\n%s", unserved, none)
	}
	h1, sentence := strings.Index(none, "</h1>"), strings.Index(none, "Manage your team")
	if h1 < 0 || sentence < h1 || strings.Contains(none[h1:sentence], "<a ") {
		t.Errorf("with no action, the title is no longer followed by the shell's sentence:\n%s", none)
	}
}

func TestTheHomeGridIsOneColumnAtEveryWidth(t *testing.T) {
	body := homeWith(t, allow(true), primaryOf(goToTasks), nil)
	class := func(node g.Node) string {
		return regexp.MustCompile(`class="([^"]*)"`).FindStringSubmatch(render(t, node))[1]
	}
	outer := class(components.Grid(components.GridProps{Columns: "1", Gap: "6"}))
	cards := class(components.Grid(components.GridProps{Columns: "1", Gap: "3"}))
	if !strings.Contains(outer, "grid-cols-1") || strings.Contains(outer, "md:") || strings.Contains(outer, "lg:") {
		t.Errorf("the home's grid compiles to %q; want one column at every width", outer)
	}
	if !strings.Contains(cards, "grid-cols-1") || strings.Contains(cards, "md:") || strings.Contains(cards, "lg:") {
		t.Errorf("a section's card grid compiles to %q; want one column at every width", cards)
	}
	if !strings.Contains(body, `class="`+outer+`"`) || !strings.Contains(body, `class="`+cards+`"`) {
		t.Errorf("the home does not lay its summary out in those grids:\n%s", body)
	}
}

func TestCountersFollowThePrimaryActionAndOnlyForTheirReader(t *testing.T) {
	counted := false
	body := homeWith(t, grants{"task:read": true}, primaryOf(goToTasks), &counted)
	if !primaryButton.MatchString(body) {
		t.Errorf("a maker who may read tasks is not shown the action:\n%s", body)
	}
	if counted {
		t.Error("the home counted people for a caller who may not read them")
	}
	for _, href := range []string{`href="/app/user/users"`, `href="/app/auth/roles"`} {
		if strings.Contains(body, href) {
			t.Errorf("the home shows %s to a caller who may not open it", href)
		}
	}
}
