package internal

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
)

// Mount is the whole shell: the assets, the five pages written by hand, the
// tenant switcher, and seven generated screens for every resource kit/rest
// registered before this module was composed.
// tenantsPath is where the switcher lives: the path the tenant module's nav
// entry names.
const tenantsPath = adminRoot + "/tenant/tenants"

func Mount(api *httpx.API, s Shell) {
	s.generated = map[string]bool{}
	s.operatorOnly = map[string]bool{}
	// Static, so a stylesheet and five scripts cost no tenant, no transaction
	// and no authorization: they are the same bytes for everybody.
	api.Static(assetPrefix, ui.Assets())

	resources := api.Resources()
	sort.Slice(resources, func(i, j int) bool { return screenPath(resources[i]) < screenPath(resources[j]) })
	for _, r := range resources {
		s.mountScreens(api, r)
	}
	s.pages(api, resources)
}

// pages are the screens no schema describes: the way in, the way around, and
// the two that are about the installation rather than about its data.
func (s *Shell) pages(api *httpx.API, resources []httpx.Resource) {
	html(api, s, "admin-login", http.MethodGet, adminRoot+"/login", "Sign in", httpx.Public(),
		func(ctx context.Context, _ *emptyInput) (*page, error) { return ok(s.login(ctx)) })

	html(api, s, "admin-dashboard", http.MethodGet, adminRoot, "The dashboard", httpx.SignedIn(),
		func(ctx context.Context, _ *emptyInput) (*page, error) { return ok(s.dashboard(ctx, resources)) })

	html(api, s, "admin-health", http.MethodGet, adminRoot+"/health", "Health", httpx.SignedIn(),
		func(ctx context.Context, _ *emptyInput) (*page, error) { return ok(s.health(ctx)) })

	html(api, s, "admin-gallery", http.MethodGet, adminRoot+"/_gallery", "The component gallery", httpx.SignedIn(),
		func(ctx context.Context, _ *emptyInput) (*page, error) { return ok(s.gallery(ctx)) })

	// The switcher lives at the path the tenant module's nav entry already
	// names, so that entry leads somewhere. It is the one page here that reads
	// across tenants, and it is declared the way that module's own routes are:
	// OperatorPermission, not Permission. The control plane is served at every
	// tenant's host, so a customer's administrator can reach this URL, and the
	// wildcard they hold in their own tenant must not answer a question about
	// everybody's. The kernel refuses it before the Authorizer is asked; the
	// sidebar drops the link for the same reason, so the two agree.
	s.generated[tenantsPath] = true
	s.operatorOnly[tenantsPath] = true
	html(api, s, "admin-tenants", http.MethodGet, tenantsPath, "The tenants of this installation",
		httpx.OperatorPermission(tenantcontracts.PermissionTenantManage),
		func(ctx context.Context, _ *emptyInput) (*page, error) { return s.tenants(ctx) })
}

// login is the way in. The form posts to the auth module's own JSON route
// rather than to a handler here: that route already mints the session cookie,
// and a second one that minted it differently is the duplicate most worth not
// having. ui/assets/js/session.js is the thirty lines that make a form post
// JSON.
func (s *Shell) login(ctx context.Context) g.Node {
	next := adminRoot
	if r, ok := httpx.RequestFrom(ctx); ok {
		if to := r.URL.Query().Get("next"); len(to) > 1 && to[0] == '/' && to[1] != '/' {
			next = to
		}
	}
	return s.bare("Sign in",
		components.Card(components.CardProps{Title: "Sign in", Description: "Use the address this tenant knows you by."}),
		components.Form(components.FormProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"data-login-form": "", "data-next": next}},
			Action: "/api/v1/auth/login", Label: "Sign in",
		},
			components.Alert(components.AlertProps{
				ComponentProps: components.ComponentProps{
					Hidden: true, Attrs: map[string]string{"data-login-error": ""}},
				Tone: "danger", Message: "", Bordered: true,
			}),
			components.Input(components.InputProps{
				Name: "email", Type: "email", Label: "Email", Required: true,
				Autocomplete: "username", AutoFocus: true, FullWidth: true}),
			components.Input(components.InputProps{
				Name: "password", Type: "password", Label: "Password", Required: true,
				Autocomplete: "current-password", FullWidth: true}),
			components.FormActions(components.FormActionsProps{},
				components.Button(components.ButtonProps{Label: "Sign in", Type: "submit", FullWidth: true})),
		),
	)
}

// dashboard is what there is and how much of it: one card per resource with the
// count its own list route would report, and the health of the instance.
func (s *Shell) dashboard(ctx context.Context, resources []httpx.Resource) g.Node {
	cards := make([]g.Node, 0, len(resources))
	for _, r := range resources {
		count := "—"
		// The count is the resource's own list, asking for no rows: the total
		// is what a page carries beside them, so this is one COUNT and not a
		// page of data thrown away.
		if _, total, err := r.List(ctx, crud.Query{Limit: 1}); err == nil {
			count = strconv.FormatInt(total, 10)
		}
		cards = append(cards, components.Card(components.CardProps{
			Title: count + " " + humanize(r.Entity) + "s", Description: "In " + r.Module,
			Clickable: true, Href: screenPath(r),
		}))
	}
	var failed []string
	for _, r := range s.checks(ctx) {
		if r.err != nil {
			failed = append(failed, r.name)
		}
	}
	tone, message := "success", "Every check passes."
	if len(failed) > 0 {
		tone, message = "danger", "Not ready: "+strings.Join(failed, ", ")
	}
	return s.page(ctx, "Dashboard",
		components.Toolbar(components.ToolbarProps{
			Title: "Dashboard", Subtitle: "What this tenant has, and whether the instance is well."}),
		components.Alert(components.AlertProps{Tone: tone, Message: message, Bordered: true}),
		components.Grid(components.GridProps{Columns: "3", Gap: "4"}, cards...),
	)
}

// health is the readiness probe with names on it. /ready answers 200 or 503 and
// names what failed; this runs the same checks and says so one at a time, which
// is what somebody looking at a broken deployment needs.
func (s *Shell) health(ctx context.Context) g.Node {
	rows := make([]components.TableRow, 0, len(s.Checks)+1)
	for _, c := range s.checks(ctx) {
		state, tone := "ok", "success"
		if c.err != nil {
			state, tone = "failing", "danger"
		}
		rows = append(rows, components.TableRow{ID: c.name, Cells: map[string]any{
			"check": c.name, "state": state, "tone": tone,
		}})
	}
	return s.page(ctx, "Health",
		components.Toolbar(components.ToolbarProps{
			Title: "Health", Subtitle: "The checks behind /ready, one at a time."}),
		components.TableWithSlots(components.TableProps{
			Columns: []components.TableColumn{
				{Key: "check", Label: "Check", Primary: true},
				{Key: "state", Label: "State"},
			},
			Rows: rows,
		}, components.TableSlots{
			Cell: func(row components.TableRow, c components.TableColumn) g.Node {
				if c.Key != "state" {
					return nil
				}
				return components.Badge(components.BadgeProps{
					Label: text(row.Cells["state"]), Tone: text(row.Cells["tone"]), Dot: true})
			},
		}),
	)
}

// result is one check and what it said.
type result struct {
	name string
	err  error
}

// checks runs every module's check, plus the database. The database one is not
// in any manifest — kit/app adds it — so it is added here for the same reason:
// an instance that cannot reach Postgres is the failure this page exists for.
//
// They run on a detached context. A check is about the instance and not about
// this tenant, and this request has already opened a tenant transaction to
// recognise its caller: a readiness query joined to that one would be a scope
// mismatch, which is what /ready never hits because a probe queries nothing
// before it. See db.Detached.
func (s *Shell) checks(ctx context.Context) []result {
	all := s.Checks
	if conn, reachable := httpx.ConnFrom(ctx); reachable {
		all = append([]health.Check{health.DatabaseCheck(conn)}, all...)
	}
	loose := db.Detached(ctx)
	out := make([]result, 0, len(all))
	for _, c := range all {
		out = append(out, result{name: c.Name(), err: c.Check(loose)})
	}
	return out
}

// gallery renders every component once, with its props, from the package's own
// exported list. It is not a fixture: ui/components' tests render the same list
// to prove every class it emits has a rule, so this page and that test cannot
// disagree about what exists.
func (s *Shell) gallery(ctx context.Context) g.Node {
	var body []g.Node
	group := ""
	for _, example := range components.Gallery() {
		if example.Group != group {
			group = example.Group
			body = append(body, components.Heading(components.HeadingProps{Text: group, Level: 2}))
		}
		body = append(body, components.Card(components.CardProps{Title: example.Name}),
			h.Div(g.Attr("data-gallery-example", example.Name), example.Node))
	}
	return s.page(ctx, "Components",
		components.Toolbar(components.ToolbarProps{
			Title: "Components", Subtitle: "Every component this application renders, once each."}),
		components.Stack(components.StackProps{Gap: "6"}, body...),
	)
}

// tenants is the switcher: every tenant of this installation and the host each
// is served at, for a person who administers more than one.
//
// It is the one cross-tenant read in this module. A tenant belongs to no
// tenant, so listing them takes a system transaction, opened on a detached
// context so it is a transaction of its own rather than a widening of the
// request's. See docs/adr/0006.
func (s *Shell) tenants(ctx context.Context) (*page, error) {
	conn, reachable := httpx.ConnFrom(ctx)
	if !reachable {
		return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	var all []*tenantcontracts.Tenant
	err := db.RunSystem(db.Detached(ctx), conn, s.Token, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		all, err = s.Tenants.List(ctx, tx)
		return err
	})
	if err != nil {
		return nil, rest.Fault(err)
	}
	rows := make([]components.TableRow, 0, len(all))
	for _, t := range all {
		rows = append(rows, components.TableRow{ID: t.ID.String(), Cells: map[string]any{
			"name": t.Name, "slug": t.Slug, "status": t.Status, "hosts": strings.Join(t.Hosts, ", "),
		}})
	}
	return ok(s.page(ctx, "Tenants",
		components.Toolbar(components.ToolbarProps{
			Title: "Tenants", Subtitle: "Every tenant of this installation. A link opens that tenant's own shell."}),
		components.TableWithSlots(components.TableProps{
			Columns: []components.TableColumn{
				{Key: "name", Label: "Tenant", Primary: true},
				{Key: "slug", Label: "Slug"},
				{Key: "status", Label: "Status"},
				{Key: "hosts", Label: "Served at"},
			},
			Rows:      rows,
			EmptyText: "No tenants. Run `platformkit bootstrap`.",
		}, components.TableSlots{
			Cell: func(row components.TableRow, c components.TableColumn) g.Node {
				switch c.Key {
				case "status":
					tone := "success"
					if text(row.Cells["status"]) != tenantcontracts.StatusActive {
						tone = "warning"
					}
					return components.Badge(components.BadgeProps{Label: text(row.Cells["status"]), Tone: tone, Dot: true})
				case "hosts":
					var links []g.Node
					for _, host := range strings.Split(text(row.Cells["hosts"]), ", ") {
						links = append(links, components.Link(components.LinkProps{
							Label: host, Href: "https://" + host + adminRoot, External: true}))
					}
					if len(links) == 0 {
						return g.Text("—")
					}
					return components.Flex(components.FlexProps{Gap: "2", Wrap: true}, links...)
				}
				return nil
			},
		}),
	))
}

// bare is a page with no navigation: the sign-in screen, shown to somebody who
// has none yet.
func (s *Shell) bare(title string, body ...g.Node) g.Node {
	return h.HTML(h.Lang("en"), head(title),
		h.Body(components.Container(components.ContainerProps{MaxWidth: "sm"},
			components.Stack(components.StackProps{Gap: "6"}, body...))))
}
