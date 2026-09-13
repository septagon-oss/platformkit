package internal

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// pages are the screens no schema describes: the way in, the way around, and
// the two that are about the installation rather than about its data. Each is
// a function of what it read and of the request, and returns a View; the
// document around it is page.Serve's.
type pages struct {
	Shell
	shell     page.Shell
	resources []httpx.Resource
}

func (p pages) mount(api *httpx.API) {
	loginShell := p.shell
	loginShell.Messages, loginShell.Locale = p.Messages, p.Locale
	page.Serve(api, loginShell, page.Route{ID: "admin-login", Method: http.MethodGet, Path: loginPath, Summary: "Sign in"},
		httpx.Public(), func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return login(ctx, r.Locale), nil
		})

	page.Serve(api, p.shell, page.Route{ID: "admin-dashboard", Method: http.MethodGet, Path: adminRoot, Summary: "The dashboard"},
		httpx.SignedIn(), func(ctx context.Context, _ page.Request, _ *page.Empty) (page.View, error) {
			return p.dashboard(ctx), nil
		})

	page.Serve(api, p.shell, page.Route{ID: "admin-health", Method: http.MethodGet, Path: healthPath, Summary: "Health"},
		httpx.SignedIn(), func(ctx context.Context, _ page.Request, _ *page.Empty) (page.View, error) {
			return healthPage(checks(ctx)), nil
		})

	p.mountGallery(api)
	p.mountDelivery(api)

	// The switcher lives at the path the tenant module's nav entry already
	// names, so that entry leads somewhere. Like delivery inspection, it reads
	// across tenants and is declared the way that module's own routes are:
	// OperatorPermission, not Permission. The control plane is served at every
	// tenant's host, so a customer's administrator can reach this URL, and the
	// wildcard they hold in their own tenant must not answer a question about
	// everybody's. The kernel refuses it before the Authorizer is asked; the
	// sidebar drops the link for the same reason, so the two agree.
	page.Serve(api, p.shell, page.Route{ID: "admin-tenants", Method: http.MethodGet, Path: tenantsPath, Summary: "The tenants of this installation"},
		httpx.OperatorPermission(tenantcontracts.PermissionTenantManage),
		func(ctx context.Context, _ page.Request, _ *page.Empty) (page.View, error) {
			return p.tenants(ctx)
		})
}

// login is the way in. The form posts to the auth module's own JSON route
// rather than to a handler here: that route already mints the session cookie,
// and a second one that minted it differently is the duplicate most worth not
// having. ui/assets/js/session.js is the thirty lines that make a form post
// JSON. It is a bare page: somebody who has no session yet has no navigation.
func login(ctx context.Context, locale *page.Locale) page.View {
	next := adminRoot
	if r, ok := httpx.RequestFrom(ctx); ok {
		// The kernel's rule, because this one used to be its own and was
		// wrong: "/\\evil.example" has a leading slash and a second character
		// that is not one, and every browser resolves it off-site.
		if to := r.URL.Query().Get("next"); httpx.LocalPath(to) {
			next = to
		}
	}
	text := func(key, fallback string) string {
		if locale == nil {
			return fallback
		}
		return locale.Text("admin.login."+key, fallback)
	}
	title := text("title", "Sign in")
	return page.View{Title: title, Bare: true, Body: []g.Node{
		components.Card(components.CardProps{Title: title, Description: text("description", "Use the address this tenant knows you by.")}),
		components.Form(components.FormProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"data-login-form": "", "data-next": next}},
			Action: "/api/v1/auth/login", Label: title,
		},
			components.Alert(components.AlertProps{
				ComponentProps: components.ComponentProps{
					Hidden: true, Attrs: map[string]string{"data-login-error": "", "lang": "en"}},
				Tone: "danger", Message: "", Bordered: true,
			}),
			components.Input(components.InputProps{
				Name: "email", Type: "email", Label: text("email", "Email"), Required: true,
				Autocomplete: "username", AutoFocus: true, FullWidth: true}),
			components.Input(components.InputProps{
				Name: "password", Type: "password", Label: text("password", "Password"), Required: true,
				Autocomplete: "current-password", FullWidth: true}),
			components.FormActions(components.FormActionsProps{},
				components.Button(components.ButtonProps{Label: title, Type: "submit", FullWidth: true})),
		),
	}}
}

// dashboard is what there is and how much of it: one card per resource the
// caller may read, with the count its own list route would report, and the
// health of the instance.
//
// The guarded count makes one authorization decision and loads no entity rows.
// A refused or unavailable count produces no card, including its resource name.
func (p pages) dashboard(ctx context.Context) page.View {
	cards := make([]g.Node, 0, len(p.resources))
	for _, r := range p.resources {
		if r.Count == nil {
			continue
		}
		total, err := r.Count(ctx)
		if err != nil {
			continue
		}
		count := strconv.FormatInt(total, 10)
		cards = append(cards, components.Card(components.CardProps{
			Title: count + " " + rest.Humanize(r.Entity) + "s", Description: "In " + r.Module,
			Clickable: true, Href: screens.Path(r, opts),
		}))
	}
	var failed []string
	for _, c := range checks(ctx) {
		if c.err != nil {
			failed = append(failed, c.name)
		}
	}
	tone, message := "success", "Every check passes."
	if len(failed) > 0 {
		tone, message = "danger", "Not ready: "+strings.Join(failed, ", ")
	}
	return page.View{Title: "Dashboard", Body: []g.Node{
		components.Toolbar(components.ToolbarProps{
			Title: "Dashboard", Subtitle: "What this tenant has, and whether the instance is well."}),
		components.Alert(components.AlertProps{Tone: tone, Message: message, Bordered: true}),
		components.Grid(components.GridProps{Columns: "3", Gap: "4"}, cards...),
	}}
}

// healthPage is the readiness probe with names on it. /ready answers 200 or 503
// and names what failed; this shows the same checks one at a time, which is
// what somebody looking at a broken deployment needs.
func healthPage(results []result) page.View {
	rows := make([]components.TableRow, 0, len(results))
	for _, c := range results {
		state, tone := "ok", "success"
		if c.err != nil {
			state, tone = "failing", "danger"
		}
		rows = append(rows, components.TableRow{ID: c.name, Cells: map[string]any{
			"check": c.name, "state": state, "tone": tone,
		}})
	}
	return page.View{Title: "Health", Body: []g.Node{
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
					Label: rest.Text(row.Cells["state"]), Tone: rest.Text(row.Cells["tone"]), Dot: true})
			},
		}),
	}}
}

// result is one check and what it said.
type result struct {
	name string
	err  error
}

// checks runs the database check, which is the one /ready runs: modules could
// contribute their own and none ever did in three repositories. It runs on a
// detached context: a check is about the instance and not about this tenant,
// and this request has already opened a tenant transaction to recognise its
// caller. See db.Detached.
func checks(ctx context.Context) []result {
	conn, reachable := httpx.ConnFrom(ctx)
	if !reachable {
		return nil
	}
	c := health.DatabaseCheck(conn)
	return []result{{name: c.Name(), err: c.Check(db.Detached(ctx))}}
}

// galleryInput is which group to show. Empty is all of them, which is the page
// the class-closure test reads.
// tenants is the switcher: every tenant of this installation and the host each
// is served at, for a person who administers more than one.
//
// It is the one cross-tenant read in this module. A tenant belongs to no
// tenant, so listing them takes a system transaction, opened on a detached
// context so it is a transaction of its own rather than a widening of the
// request's. See docs/adr/0006.
func (p pages) tenants(ctx context.Context) (page.View, error) {
	conn, reachable := httpx.ConnFrom(ctx)
	if !reachable {
		return page.View{}, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	var all []*tenantcontracts.Tenant
	err := db.RunSystem(db.Detached(ctx), conn, p.Token, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		all, err = p.Tenants.List(ctx, tx)
		return err
	})
	if err != nil {
		return page.View{}, rest.Fault(err)
	}
	rows := make([]components.TableRow, 0, len(all))
	for _, t := range all {
		rows = append(rows, components.TableRow{ID: t.ID.String(), Cells: map[string]any{
			"name": t.Name, "slug": t.Slug, "status": t.Status, "hosts": strings.Join(t.Hosts, ", "),
		}})
	}
	return page.View{Title: "Tenants", Body: []g.Node{
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
					if rest.Text(row.Cells["status"]) != tenantcontracts.StatusActive {
						tone = "warning"
					}
					return components.Badge(components.BadgeProps{Label: rest.Text(row.Cells["status"]), Tone: tone, Dot: true})
				case "hosts":
					var links []g.Node
					for _, host := range strings.Split(rest.Text(row.Cells["hosts"]), ", ") {
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
	}}, nil
}
