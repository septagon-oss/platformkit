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
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

// writtenHere is the language of the copy written in this file. Every word on the
// pages below is a Go string rather than a catalogue key, so each says which
// language it is in rather than wearing the request's: document.View.Language
// exists for exactly that, and a page that declared a language its sentences are
// not written in would send a screen reader off with the wrong voice and tell a
// translation tool the work is done. A page whose copy comes from a catalogue says
// nothing here and takes the language that was negotiated — see signIn, which
// reads its labels out of admin.login.* and declares nothing.
const writtenHere = "en"

// pages are the screens no schema describes: the way in, the way around, and
// the two that are about the installation rather than about its data. Each is
// a function of what it read and of the request, and returns a View; the
// document around it is page.Serve's.
type pages struct {
	Shell
	at        addresses
	shell     page.Shell
	nav       page.Navigation
	resources []httpx.Resource
	declared  []tenancy.Grant
}

func (p pages) mount(s httpx.Surfaces, home, app *httpx.Router) {
	// The sign-in page needs no separate wiring: it is mounted on the shell, and
	// the shell carries the catalog and the preference resolver.
	loginShell := p.shell
	page.Serve(app, loginShell, page.Route{ID: "admin-login", Method: http.MethodGet, Path: p.at.login.rel, Summary: "Sign in"},
		httpx.Public(), func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return login(ctx, r.Locale, p.at.dashboard.at, p.SignIn, p.at.forgot.at), nil
		})

	page.Serve(home, p.shell, page.Route{ID: "admin-dashboard", Method: http.MethodGet, Path: p.at.dashboard.rel, Summary: "The dashboard"},
		httpx.SignedIn(), func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return p.dashboard(ctx, r.Tenant), nil
		})

	page.Serve(app, p.shell, page.Route{ID: "admin-health", Method: http.MethodGet, Path: p.at.health.rel, Summary: "Health"},
		httpx.SignedIn(), func(ctx context.Context, _ page.Request, _ *page.Empty) (page.View, error) {
			return healthPage(checks(ctx)), nil
		})

	p.mountGallery(app)
	p.mountRoles(s.App)
	p.mountSessions(s.App)
	p.mountPasswordDoors(app)

	// The switcher lives at the path the tenant module's nav entry already
	// names, so that entry leads somewhere. It is the one page here that reads
	// across tenants, and it is declared the way that module's own routes are:
	// OperatorPermission, not Permission. The control plane is served at every
	// tenant's host, so a customer's administrator can reach this URL, and the
	// wildcard they hold in their own tenant must not answer a question about
	// everybody's. The kernel refuses it before the Authorizer is asked; the
	// sidebar drops the link for the same reason, so the two agree.
	page.Serve(namespace(s.App, "tenant"), p.shell, page.Route{ID: "admin-tenants", Method: http.MethodGet, Path: p.at.tenants.rel, Summary: "The tenants of this installation"},
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
func login(ctx context.Context, locale *page.Locale, next, action, forgot string) page.View {
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
			Action: action, Label: title,
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
		// The way in for everybody the password did not answer: the person whose
		// address is not here, the one whose account is waiting to be verified, and
		// the one who was invited and has not chosen a password yet. All three are
		// refused the same sentence, and this link is what that sentence points at —
		// asking for a link is the step that answers every one of them, because the
		// door re-issues to an invited address and says the same thing to an unknown
		// one. It is always offered, whatever the composition wires: forgetting a
		// password is not an opt-in.
		components.Flex(components.FlexProps{Direction: "row", Gap: "2"},
			components.Link(components.LinkProps{Href: forgot, Variant: "text",
				Label: text("forgot", "Forgot your password?")})),
	}}
}

// mountPasswordDoors mounts the two pages a person who cannot sign in needs: the
// one that asks for a link, and the one the link leads to.
//
// Both exist only when the auth module is composed. A "Forgot your password?"
// that posts to a route nobody mounted is a door painted on a wall, and a
// composition with no auth module has no password to forget — so that check is
// also what decides whether the two addresses count as served.
//
// Both forms post JSON to the auth module's own routes rather than to a handler
// here, for the sign-in page's reason: that module already mints and consumes the
// credential, and a second path that did it differently is the duplicate worth not
// having. ui/assets/js/session.js has known the forgot and reset form kinds since
// it was written; what was missing was any page that rendered one.
func (p pages) mountPasswordDoors(app *httpx.Router) {
	if !app.Known("auth") {
		return
	}
	// Unlike Deps.SignIn, which the composition names because it is the pinned
	// session door, these are the plain composed addresses: the workspace puts a
	// module's API under its own name, and asking the kernel where that is beats
	// writing another module's prefix down here.
	auth := namespace(app, "auth")
	ask, set := auth.Path("/password/forgot"), auth.Path("/password/reset")

	page.Serve(app, p.shell, page.Route{ID: "admin-forgot", Method: http.MethodGet, Path: p.at.forgot.rel,
		Summary: "Ask for a link to set a new password"}, httpx.Public(),
		func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return forgotPassword(ctx, r.Locale, ask, p.at.login.at), nil
		})

	// The set-password page is mounted in auth's namespace, because it is the
	// address the auth module mails: its ResetPath is this workspace screen, and a
	// link that led anywhere else is a mail whose page answers nothing.
	page.Serve(auth, p.shell, page.Route{ID: "admin-set-password", Method: http.MethodGet, Path: p.at.reset.rel,
		Summary: "Choose a new password"}, httpx.Public(),
		func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return setPassword(ctx, r.Locale, set, p.at.login.at), nil
		})
}

// forgot asks for one thing and promises the answer it can honestly make: the same
// sentence whether or not the address is known here, which is the enumeration rule
// the request route keeps for the same reason.
func forgotPassword(ctx context.Context, locale *page.Locale, action, back string) page.View {
	text := func(key, fallback string) string {
		if locale == nil {
			return fallback
		}
		return locale.Text("admin.forgot."+key, fallback)
	}
	title := text("title", "Forgot your password?")
	return page.View{Title: title, Bare: true, Body: []g.Node{
		components.Card(components.CardProps{Title: title, Description: text("description",
			"Enter the address this tenant knows you by. If it can receive an account email, a link that sets a new password is sent to it. The link works once and stops working in an hour.")}),
		components.Form(components.FormProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-auth-form": "forgot"}},
			Action:         action, Label: title,
		},
			components.Alert(components.AlertProps{
				ComponentProps: components.ComponentProps{
					Hidden: true, Attrs: map[string]string{"data-auth-error": "", "lang": "en"}},
				Tone: "danger", Message: "", Bordered: true,
			}),
			components.Alert(components.AlertProps{
				ComponentProps: components.ComponentProps{
					Hidden: true, Attrs: map[string]string{"data-auth-message": "", "role": "status", "lang": "en"}},
				Tone: "success", Bordered: true,
				Message: text("sent", "If this address can receive an account email, a link will be sent. Check your inbox."),
			}),
			components.Input(components.InputProps{
				Name: "email", Type: "email", Label: text("email", "Email"), Required: true,
				Autocomplete: "username", AutoFocus: true, FullWidth: true}),
			components.FormActions(components.FormActionsProps{},
				components.Button(components.ButtonProps{Label: text("submit", "Send me a link"), Type: "submit", FullWidth: true})),
		),
		backToSignIn(text, back),
	}}
}

// setPassword is the other end of that link, for an invitation and a reset alike:
// the person who was invited and the person who forgot both arrive here with a
// token in the address, and both leave with a password they chose. The token stays
// out of the form's fields — session.js reads it from the address and rewrites the
// URL so it is not left in the location bar — because a token in a text input is
// one a password manager offers to save.
func setPassword(ctx context.Context, locale *page.Locale, action, back string) page.View {
	text := func(key, fallback string) string {
		if locale == nil {
			return fallback
		}
		return locale.Text("admin.reset."+key, fallback)
	}
	title := text("title", "Choose a password")
	return page.View{Title: title, Bare: true, Body: []g.Node{
		components.Card(components.CardProps{Title: title, Description: text("description",
			"This link sets a password once. Saving it ends every session already open for this account.")}),
		components.Form(components.FormProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"data-auth-form": "reset", "data-next": back}},
			Action: action, Label: title,
		},
			components.Alert(components.AlertProps{
				ComponentProps: components.ComponentProps{
					Hidden: true, Attrs: map[string]string{"data-auth-error": "", "lang": "en"}},
				Tone: "danger", Message: "", Bordered: true,
			}),
			components.Input(components.InputProps{
				Name: "new", Type: "password", Label: text("password", "New password"), Required: true,
				MinLength: 12, AutoFocus: true, FullWidth: true, Autocomplete: "new-password",
				HelpText: text("password.help", "At least twelve characters.")}),
			components.FormActions(components.FormActionsProps{},
				components.Button(components.ButtonProps{Label: text("submit", "Save the password"), Type: "submit", FullWidth: true})),
		),
		backToSignIn(text, back),
	}}
}

// backToSignIn is the way out of a bare page. A person who followed a link they
// were not expecting, or whose address is not known here, is not trapped on the
// page that refused them.
func backToSignIn(text func(key, fallback string) string, back string) g.Node {
	return components.Flex(components.FlexProps{Direction: "row", Gap: "2"},
		components.Link(components.LinkProps{Href: back, Variant: "text",
			Label: text("signIn", "Back to sign in")}))
}

// dashboard is what there is and how much of it: one card per resource the
// caller may read, with the count its own list route would report, and the
// health of the instance.
//
// The guarded count makes one authorization decision and loads no entity rows.
// A refused or unavailable count produces no card, including its resource name.
//
// A caller who may read no counts gets what their role opens rather than an
// empty grid — see contents. This page is where a signed-in person arrives, so it
// is the page that has to have something to offer them.
func (p pages) dashboard(ctx context.Context, t tenancy.Tenant) page.View {
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
			Clickable: true, Href: r.Screen,
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
	// Language says what this page's words are written in, which is not the
	// request's language: every word on this page — the title, the subtitle, the
	// health alert, the counts under the cards — is a Go string in this file, and no
	// catalogue is consulted for any of them. A page declares the language of the
	// copy it shows (see document.View.Language, and the refusal page in ui/document
	// which says the same about its own words), so a browser, a screen reader and a
	// translation tool are told the truth: this one is English, whichever language
	// the tenant is served in. The day any of this copy goes into a catalogue, this
	// line goes with it.
	return page.View{Title: "Dashboard", Language: writtenHere, Body: []g.Node{
		components.Toolbar(components.ToolbarProps{
			Title: "Dashboard", Subtitle: "What this tenant has, and whether the instance is well."}),
		components.Alert(components.AlertProps{Tone: tone, Message: message, Bordered: true}),
		p.contents(ctx, t, cards),
	}}
}

// contents is the body under the two headings: the counts, or — for the caller
// the tenant holds no counts for — the screens their role actually opens.
//
// The grid alone is the page twelve of the eighteen apps in the walkthrough of
// record scored nothing on: a person signed in, was welcomed by a heading, an
// alert and a grid with no card in it, and had nothing on that page to do. Their
// role does open something, usually — a screen a module writes by hand, like the
// role list, carries no count to draw — so the page names those screens. They come
// from page.Navigation.Visible, the same read the sidebar renders and the one that
// asks the Authorizer the routes enforce with, so a page cannot name a door that
// answers 403. Where the role opens nothing at all the page says so, and says who
// can change it, which is a fact a person can act on; an empty grid is not.
func (p pages) contents(ctx context.Context, t tenancy.Tenant, cards []g.Node) g.Node {
	if len(cards) > 0 {
		return components.Grid(components.GridProps{Columns: "3", Gap: "4"}, cards...)
	}
	visible := p.nav.Visible(ctx, t, p.Authorize)
	if len(visible) == 0 {
		return components.EmptyState(components.EmptyStateProps{
			Title:       "No screens open yet",
			Description: "Your role in this tenant opens no page. An administrator can grant it the permissions you need.",
			Bordered:    true,
		})
	}
	ops := make([]g.Node, 0, len(visible))
	for _, e := range visible {
		ops = append(ops, components.Link(components.LinkProps{Href: e.Screen, Label: e.Label}))
	}
	return components.EmptyStateWithSlots(components.EmptyStateProps{
		Title:       "What your role opens",
		Description: "This tenant holds nothing this role can count. These screens are yours.",
		Bordered:    true,
	}, components.EmptyStateSlots{Actions: ops})
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
	return page.View{Title: "Health", Language: writtenHere, Body: []g.Node{
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
		return page.View{}, unreachable
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
							Label: host, Href: "https://" + host + p.at.workspace.at, External: true}))
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
