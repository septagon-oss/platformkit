package internal

import (
	"context"
	"github.com/septagon-oss/platformkit/kit/screen"
	admincontracts "github.com/septagon-oss/platformkit/modules/admin/contracts"
	"net/http"
	"strconv"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
	"github.com/septagon-oss/platformkit/ui/style"
)

// pages are the screens no schema describes: the way in, the way around, and
// the two that are about the installation rather than about its data. Each is
// a function of what it read and of the request, and returns a View; the
// document around it is page.Serve's.
type pages struct {
	Shell
	at    addresses
	shell page.Shell
	nav   page.Navigation
	// primary holds the application's primary action, or nothing.
	primary   page.Navigation
	resources []httpx.Resource
	declared  []tenancy.Grant
}

func (p pages) mount(s httpx.Surfaces, home, app *httpx.Router) {
	// The sign-in page needs no separate wiring: it is mounted on the shell, and
	// the shell carries the catalog and the preference resolver.
	loginShell := p.shell
	page.Serve(app, loginShell, page.Route{View: admincontracts.Screen("login"), Targets: []screen.Target{{OperationID: p.SignInOperationID, Method: "POST", Path: p.SignIn}}, ID: "admin-login", Method: http.MethodGet, Path: p.at.login.rel, Summary: "Sign in"},
		httpx.Public(), func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			var register string
			if p.Registration != nil {
				register = p.at.register.at
			}
			return p.login(ctx, r, p.at.dashboard.at, p.SignIn, p.at.forgot.at, register), nil
		})

	page.Serve(home, p.shell, page.Route{View: admincontracts.Screen("dashboard"), ID: "admin-dashboard", Method: http.MethodGet, Path: p.at.dashboard.rel, Summary: "The dashboard"},
		httpx.SignedIn(), func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return p.dashboard(ctx, r.Tenant, resource.Options{Locale: r.Locale}), nil
		})

	page.Serve(app, p.shell, page.Route{View: admincontracts.Screen("health"), ID: "admin-health", Method: http.MethodGet, Path: p.at.health.rel, Summary: "Health"},
		httpx.SignedIn(), func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return healthPage(checks(ctx), resource.Options{Locale: r.Locale}), nil
		})

	p.mountGallery(app)
	p.mountRoles(s.App)
	p.mountSessions(s.App)
	p.mountPasswordDoors(s.App, s.Public)
	p.mountRegistration(app)

	// The switcher lives at the path the tenant module's nav entry already
	// names, so that entry leads somewhere. It is the one page here that reads
	// across tenants, and it is declared the way that module's own routes are:
	// OperatorPermission, not Permission. The control plane is served at every
	// tenant's host, so a customer's administrator can reach this URL, and the
	// wildcard they hold in their own tenant must not answer a question about
	// everybody's. The kernel refuses it before the Authorizer is asked; the
	// sidebar drops the link for the same reason, so the two agree.
	page.Serve(namespace(s.App, "tenant"), p.shell, page.Route{View: admincontracts.Screen("tenants"), ID: "admin-tenants", Method: http.MethodGet, Path: p.at.tenants.rel, Summary: "The tenants of this installation"},
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
// The destination under the form is where the person goes once the sign-in
// answers. Three callers, three answers, in that order:
//
//   - Somebody pushed off a specific screen (`?next=`) goes back to it, if the
//     kernel's own local-path rule accepts it.
//   - Somebody who is already signed in and is standing at the sign-in page goes
//     to the first screen their role opens. They do not need a second sign-in,
//     and the page that asks for one anyway is a dead end with a form on it.
//   - Everybody else gets the shell's own home, which is the page that reads
//     like their role — see TestTheDashboardNamesTheScreensThisCallersRoleOpens
//     for why the landing stays there, and what answers the walkthrough's zero
//     scores once they arrive.
//
// The button beside the form is a second way through the same door, drawn as a
// secondary action because the page asks for one thing at a time: "Sign in" stays
// the only filled button above the fold, and a person who would rather not type a
// password at all has the control in front of them. It carries the four ceremony
// addresses the auth module publishes, derived from the login address the
// composition already handed this page — p.SignIn is that route, so its legs are
// the same paths with the ceremony appended, and this page invents no route and
// names no module. Which pair the control runs is the module's answer, not the
// page's guess: with no password offered it is the usernameless ceremony, and
// after /login refused to finish without a second factor it is the challenge
// ceremony for the address in the form. What the browser does with either pair is
// ui/assets/js/passkeys.js: hand the options to the platform, hand the answer
// back. A tenant that never opened the usernameless door answers the begin leg
// with the reason, and that sentence is what the page shows.
func (p pages) login(ctx context.Context, r page.Request, next, action, forgot, register string) page.View {
	home := p.at.dashboard.at
	locale := r.Locale
	asked := ""
	if h, ok := httpx.RequestFrom(ctx); ok {
		// The kernel's rule, because this one used to be its own and was
		// wrong: "/\\evil.example" has a leading slash and a second character
		// that is not one, and every browser resolves it off-site.
		if to := h.URL.Query().Get("next"); httpx.LocalPath(to) {
			asked = to
		}
	}
	carried := sessionRanOut(ctx)
	if asked != "" {
		next = asked
	} else if open := p.firstScreen(ctx, r); open != "" {
		next = open
	}
	text := func(key, fallback string) string {
		if locale == nil {
			return fallback
		}
		return locale.Text("admin.login."+key, fallback)
	}
	ways := loginWays(text, forgot, register)
	title := text("title", "Sign in")
	// Someone pushed off a screen is told to come back to it, not asked who they are.
	again := text("description", "Use the address this tenant knows you by.")
	if carried {
		again = text("again", "Sign in again to continue.")
	}
	body := []g.Node{
		components.SectionHeader(components.SectionHeaderProps{Eyebrow: fallback(r.Tenant.Name, brand), Title: title,
			Description: again, Level: 1, Size: 2}),
		components.Form(components.FormProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				// home travels next to next because the two can be different
				// people: next is the screen the *caller of this page* opens,
				// and the form below can answer with somebody else — the
				// second tab that switches account. See session.js.
				"data-login-form": "", "data-next": next, "data-home": home}},
			Action: action, Label: title,
		},
			components.Alert(components.AlertProps{
				ComponentProps: components.ComponentProps{
					Hidden: true, Attrs: map[string]string{"data-login-error": "", "lang": "en"}},
				Tone: "danger", Message: "", Bordered: true,
			}),
			components.Input(components.InputProps{
				ComponentProps: components.ComponentProps{Attrs: screenAnchor("login", "field.email")},
				Name:           "email", Type: "email", Label: text("email", "Email"), Required: true,
				Autocomplete: "username", AutoFocus: true, FullWidth: true}),
			components.Input(components.InputProps{
				ComponentProps: components.ComponentProps{Attrs: screenAnchor("login", "field.password")},
				Name:           "password", Type: "password", Label: text("password", "Password"), Required: true,
				Autocomplete: "current-password", FullWidth: true}),
			components.FormActions(components.FormActionsProps{},
				components.Button(components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: screenAnchor("login", "action.submit")}, Label: title, Type: "submit", FullWidth: true})),
		),
		// The way in for everybody the password did not answer: the person whose
		// address is not here, the one whose account is waiting to be verified, and
		// the one who was invited and has not chosen a password yet. All three are
		// refused the same sentence, and this link is what that sentence points at —
		// asking for a link is the step that answers every one of them, because the
		// door re-issues to an invited address and says the same thing to an unknown
		// one. It is always offered, whatever the composition wires: forgetting a
		// password is not an opt-in.
		components.Flex(components.FlexProps{Direction: "row", Gap: "2"}, ways...),
		components.Divider(components.DividerProps{Text: text("or", "or")}),
		components.Button(components.ButtonProps{
			Label: text("passkey", "Continue with a passkey"), FullWidth: true, Variant: "outline",
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"data-passkey-signin": "",
				"data-begin":          action + "/passkey/begin",
				"data-verify":         action + "/passkey/verify",
				"data-factor-begin":   strings.TrimSuffix(action, "/login") + "/challenge/passkey/begin",
				"data-factor-verify":  strings.TrimSuffix(action, "/login") + "/challenge/passkey/verify",
				"data-next":           next,
			}},
		}),
	}
	return accessPage(title, body...)
}

// sessionRanOut reports whether the request presents a session the server no longer
// answers: a session cookie with nobody resolved behind it. It is the only trace of an
// expired session the server can see, and enough to say "sign in again" rather than
// introduce the form to a stranger. It is read from the request, never from the query:
// a link must not be able to claim that a session ended.
func sessionRanOut(ctx context.Context) bool {
	h, ok := httpx.RequestFrom(ctx)
	if !ok {
		return false
	}
	_, signedIn := tenancy.PrincipalFrom(ctx)
	_, carried := httpx.SessionCookieOf(h)
	return carried && !signedIn
}

// accessPage keeps the introduction, form and recovery links in one surface.
func accessPage(title string, body ...g.Node) page.View {
	return page.View{Title: title, Bare: true, Body: []g.Node{
		components.CardWithSlots(components.CardProps{}, components.CardSlots{Content: []g.Node{
			components.Stack(components.StackProps{Gap: "6"}, body...),
		}}),
	}}
}

// registerPage is the way in for somebody the tenant has no account for yet, and
// it exists only when the composition said a stranger may make one — see
// admin.Deps.Registration. The form posts to the registration door itself, as the
// other three do: the module behind that door owns who becomes a person here and
// in what state, and a second path that decided it would be a second answer.
//
// The copy says what the state is, because it is the part a person cannot see:
// a password-first sign-up leaves an account that cannot sign in until the
// mailbox link is opened, and a form that promises otherwise sends somebody who
// did nothing wrong to a refusal they cannot explain.
func registerPage(ctx context.Context, locale *page.Locale, kind RegistrationKind, action, back string) page.View {
	text := func(key, fallback string) string {
		if locale == nil {
			return fallback
		}
		return locale.Text("admin.register."+key, fallback)
	}
	title := text("title", "Create an account")
	slots := []g.Node{
		components.Alert(components.AlertProps{
			ComponentProps: components.ComponentProps{
				Hidden: true, Attrs: map[string]string{"data-auth-error": "", "lang": "en"}},
			Tone: "danger", Message: "", Bordered: true,
		}),
		// No lang on the acknowledgment: its sentence comes from the catalogue, so
		// in a translated tenant it is that language and takes the document's
		// declaration the way the card's description does. See forgotPassword.
		components.Alert(components.AlertProps{
			ComponentProps: components.ComponentProps{
				Hidden: true, Attrs: map[string]string{"data-auth-message": "", "role": "status"}},
			Tone: "success", Bordered: true,
			Message: text("sent", "If this address can receive an account email, a link will be sent. Check your inbox."),
		}),
		components.Input(components.InputProps{
			ComponentProps: components.ComponentProps{Attrs: screenAnchor("register", "field.email")},
			Name:           "email", Type: "email", Label: text("email", "Email"), Required: true,
			Autocomplete: "username", AutoFocus: true, FullWidth: true}),
		components.Input(components.InputProps{
			ComponentProps: components.ComponentProps{Attrs: screenAnchor("register", "field.displayName")},
			Name:           "displayName", Type: "text", Label: text("name", "Name"),
			Autocomplete: "name", FullWidth: true}),
	}
	// The two doors take different fields, and session.js already tells them apart
	// by the form's kind alone.
	if kind == RegistrationKindPassword {
		slots = append(slots,
			components.Input(components.InputProps{
				ComponentProps: components.ComponentProps{Attrs: screenAnchor("register", "field.password")},
				Name:           "password", Type: "password", Label: text("password", "Password"), Required: true,
				MinLength: 12, FullWidth: true, Autocomplete: "new-password"}),
			components.Input(components.InputProps{
				ComponentProps: components.ComponentProps{Attrs: screenAnchor("register", "field.confirmation")},
				Name:           "confirmation", Type: "password", Label: text("confirmation", "Confirm the password"),
				Required: true, MinLength: 12, FullWidth: true, Autocomplete: "new-password"}),
			components.Checkbox(components.CheckboxProps{
				ComponentProps: components.ComponentProps{Attrs: screenAnchor("register", "field.termsAccepted")},
				Name:           "termsAccepted", Label: text("terms", "I accept the terms of this tenant."),
				Required: true}),
		)
	}
	slots = append(slots, components.FormActions(components.FormActionsProps{},
		components.Button(components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: screenAnchor("register", "action.submit")}, Label: text("submit", "Create the account"),
			Type: "submit", FullWidth: true})))
	return accessPage(title,
		components.SectionHeader(components.SectionHeaderProps{Title: title, Level: 1, Size: 2, Description: text("description",
			"Tell us who you are. If the address is not here yet, an account is made for it.")}),
		components.Form(components.FormProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-auth-form": kind.form()}},
			Action:         action, Label: title,
		}, slots...),
		backToSignIn(text, back, "register"),
	)
}

// verifyEmailPage is the other half of a sign-up: the link the mailbox carries
// opens this page, which spends the token and sends the person on to sign in. It
// is mounted because the mail is sent — an address the mail links to that answers
// 404 is a sign-up with no way to finish it.
//
// The token never reaches a field: session.js reads it out of the address, clears
// it from the location bar and posts it, so the one bearer in this page's address
// is not also in its markup, its history or a password manager's offer.
func verifyEmailPage(ctx context.Context, locale *page.Locale, action, back string) page.View {
	text := func(key, fallback string) string {
		if locale == nil {
			return fallback
		}
		return locale.Text("admin.verify."+key, fallback)
	}
	title := text("title", "Confirm your email address")
	return accessPage(title,
		components.SectionHeader(components.SectionHeaderProps{Title: title, Level: 1, Size: 2, Description: text("description",
			"This link confirms the address and turns on the account. It works once and expires in twenty-four hours.")}),
		components.Form(components.FormProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"data-auth-form": "verify-email", "data-next": back}},
			Action: action, Label: title,
		},
			components.Alert(components.AlertProps{
				ComponentProps: components.ComponentProps{
					Hidden: true, Attrs: map[string]string{"data-auth-error": "", "lang": "en"}},
				Tone: "danger", Message: "", Bordered: true,
			}),
			components.FormActions(components.FormActionsProps{},
				components.Button(components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: screenAnchor("verify-email", "action.submit")}, Label: text("submit", "Confirm the address"), Type: "submit", FullWidth: true})),
		),
		backToSignIn(text, back, "verify-email"),
	)
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
func (p pages) mountPasswordDoors(app, public *httpx.Router) {
	if !app.Known("auth") {
		return
	}
	// Unlike Deps.SignIn, which the composition names because it is the pinned
	// session door, these are the plain composed addresses: the workspace puts a
	// module's API under its own name, and asking the kernel where that is beats
	// writing another module's prefix down here.
	//
	// The two doors are asked of two routers, because auth mounts them on two
	// surfaces: the set-password door answers on the workspace, the request for a
	// link on the public face. Asking the workspace router for the second one is not
	// harmless — /api/v1/auth/password/forgot is an alias row (kit/httpx/aliases.go)
	// and answers 307, and ui/assets/js/session.js posts with redirect: "error", so a
	// person pressing "Forgot your password?" would be told the outcome is unknown and
	// nothing would be sent. A form posts to the address that answers it.
	auth, asks := namespace(app, "auth"), namespace(public, "auth")
	ask, set := asks.Path("/password/forgot"), auth.Path("/password/reset")

	page.Serve(app, p.shell, page.Route{View: admincontracts.Screen("forgot"), ID: "admin-forgot", Method: http.MethodGet, Path: p.at.forgot.rel,
		Summary: "Ask for a link to set a new password"}, httpx.Public(),
		func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return forgotPassword(ctx, r.Locale, ask, p.at.login.at), nil
		})

	// The set-password page is mounted in auth's namespace, because it is the
	// address the auth module mails: its ResetPath is this workspace screen, and a
	// link that led anywhere else is a mail whose page answers nothing.
	page.Serve(auth, p.shell, page.Route{View: admincontracts.Screen("set-password"), ID: "admin-set-password", Method: http.MethodGet, Path: p.at.reset.rel,
		Summary: "Choose a new password"}, httpx.Public(),
		func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return setPassword(ctx, r.Locale, set, p.at.login.at, p.at.dashboard.at), nil
		})

	// The same argument for the confirmation link a sign-up mails: auth names
	// this workspace screen in that mail, so this is where the mount goes.
	page.Serve(auth, p.shell, page.Route{View: admincontracts.Screen("verify-email"), ID: "admin-verify-email", Method: http.MethodGet, Path: p.at.verify.rel,
		Summary: "Confirm an email address"}, httpx.Public(),
		func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return verifyEmailPage(ctx, r.Locale, asks.Path("/verify-email"), p.at.login.at), nil
		})
}

// mountRegistration mounts the page a stranger signs up on, for a composition
// that named the door the form posts to. The link on the sign-in card and this
// mount read the same Deps.Registration, so neither can offer what the other did
// not build: a form that posts to an address nothing mounted answers is the door
// painted on a wall, and a route with no link to it is a way in nobody finds.
func (p pages) mountRegistration(app *httpx.Router) {
	if p.Registration == nil {
		return
	}
	page.Serve(app, p.shell, page.Route{View: admincontracts.Screen("register"), Targets: []screen.Target{{OperationID: p.Registration.OperationID, Method: "POST", Path: p.Registration.Address}}, ID: "admin-register", Method: http.MethodGet, Path: p.at.register.rel,
		Summary: "Create an account"}, httpx.Public(),
		func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return registerPage(ctx, r.Locale, p.Registration.Kind, p.Registration.Address, p.at.login.at), nil
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
	return accessPage(title,
		components.SectionHeader(components.SectionHeaderProps{Title: title, Level: 1, Size: 2, Description: text("description",
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
			// No lang here, and the acknowledgment follows the negotiated language: this
			// sentence comes from the catalogue, so in a pt-PT request it is
			// Portuguese and it takes the document's negotiated language the way the
			// card's description and every label on the page do. Declaring "en" over
			// Portuguese copy is a screen reader reading Portuguese with an English
			// voice. The error alert above keeps its lang="en" because what arrives
			// there is session.js's own English sentence, not this page's copy.
			components.Alert(components.AlertProps{
				ComponentProps: components.ComponentProps{
					Hidden: true, Attrs: map[string]string{"data-auth-message": "", "role": "status"}},
				Tone: "success", Bordered: true,
				Message: text("sent", "If this address can receive an account email, a link will be sent. Check your inbox."),
			}),
			components.Input(components.InputProps{
				ComponentProps: components.ComponentProps{Attrs: screenAnchor("forgot", "field.email")},
				Name:           "email", Type: "email", Label: text("email", "Email"), Required: true,
				Autocomplete: "username", AutoFocus: true, FullWidth: true}),
			components.FormActions(components.FormActionsProps{},
				components.Button(components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: screenAnchor("forgot", "action.submit")}, Label: text("submit", "Send me a link"), Type: "submit", FullWidth: true})),
		),
		backToSignIn(text, back, "forgot"),
	)
}

// firstScreen is the first page this caller's role opens, and "" when nobody is
// signed in, the host resolved no tenant, or their role opens nothing they are
// not already standing on.
//
// The two pages this shell writes about itself — its home and its health check —
// are not what a role *opens*: every signed-in person can reach them whatever
// they hold, so naming one of them would answer "the first screen your role
// opens" with the page a person with no grants at all also reaches. What is left
// is the list the sidebar renders, in the sidebar's own order, from
// page.Navigation.Visible, which asks the same Authorizer the routes enforce with.
// A screen it names is a screen that answers this person 200; a screen no module
// serves is already dropped by that read.
func (p pages) firstScreen(ctx context.Context, r page.Request) string {
	if !r.SignedIn || p.Authorize == nil || r.Tenant.ID == uuid.Nil {
		return ""
	}
	for _, e := range p.nav.Visible(ctx, r.Tenant, p.Authorize) {
		if e.Screen == p.at.dashboard.at || e.Screen == p.at.health.at {
			continue
		}
		return e.Screen
	}
	return ""
}

// setPassword is the other end of that link, for an invitation and a reset alike:
// the person who was invited and the person who forgot both arrive here with a
// token in the address, and both leave with a password they chose — and with a
// session, because spending the link is the sign-in this credential is. So the
// form's destination is the workspace, not the sign-in page it came from: a page
// that sets a password and then asks for one is the half-door the front door was
// scored zero on. `back` stays the link under the form for the one account the
// answer leaves signed out — a person who enrolled a second factor finishes at the
// sign-in form — and for anybody who decides not to use the link at all.
//
// The token stays out of the form's fields — session.js reads it from the address
// and rewrites the URL so it is not left in the location bar — because a token in
// a text input is one a password manager offers to save.
func setPassword(ctx context.Context, locale *page.Locale, action, back, home string) page.View {
	text := func(key, fallback string) string {
		if locale == nil {
			return fallback
		}
		return locale.Text("admin.reset."+key, fallback)
	}
	title := text("title", "Choose a password")
	return accessPage(title,
		// The twelve-character rule is in this sentence rather than in a help line
		// under the field: help text renders at the sheet's smallest step, and a
		// page whose <p> copy comes in three sizes is a design-gate refusal — body
		// size is constant down the page. The field still carries minLength, so the
		// browser refuses a short password where it is typed.
		components.SectionHeader(components.SectionHeaderProps{Title: title, Level: 1, Size: 2, Description: text("description",
			"This link sets a password once. Saving it ends every session already open for this account. A password is at least twelve characters.")}),
		components.Form(components.FormProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"data-auth-form": "reset", "data-next": home}},
			Action: action, Label: title,
		},
			components.Alert(components.AlertProps{
				ComponentProps: components.ComponentProps{
					Hidden: true, Attrs: map[string]string{"data-auth-error": "", "lang": "en"}},
				Tone: "danger", Message: "", Bordered: true,
			}),
			components.Input(components.InputProps{
				ComponentProps: components.ComponentProps{Attrs: screenAnchor("set-password", "field.new")},
				Name:           "new", Type: "password", Label: text("password", "New password"), Required: true,
				MinLength: 12, AutoFocus: true, FullWidth: true, Autocomplete: "new-password"}),
			components.FormActions(components.FormActionsProps{},
				components.Button(components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: screenAnchor("set-password", "action.submit")}, Label: text("submit", "Save the password"), Type: "submit", FullWidth: true})),
		),
		backToSignIn(text, back, "set-password"),
	)
}

// backToSignIn is the way out of a bare page. A person who followed a link they
// were not expecting, or whose address is not known here, is not trapped on the
// page that refused them.
func backToSignIn(text func(key, fallback string) string, back, view string) g.Node {
	return components.Flex(components.FlexProps{Direction: "row", Gap: "2"},
		components.Link(components.LinkProps{ComponentProps: components.ComponentProps{Attrs: screenAnchor(view, "action.back")}, Href: back, Variant: "text",
			Label: text("signIn", "Back to sign in")}))
}

// workspaceScope names whose workspace the request's host serves: the
// installation's operator or one tenant. It follows the host, never the
// caller's grants.
func workspaceScope(t tenancy.Tenant, o resource.Options) string {
	if t.Operator {
		return o.Text("admin.workspace.operator", "Platform operator")
	}
	return o.Text("admin.workspace.tenant_workspace", "Tenant workspace")
}

// dashboard is the workspace home: the application's primary action first, when
// it declared one and this caller may follow it, then every destination the
// caller may open, grouped as the sidebar groups them, with the count its own
// list reports. A destination whose count fails stays on the page without a
// figure, and the operator's health line appears only when a check was observed.
func (p pages) dashboard(ctx context.Context, t tenancy.Tenant, o resource.Options) page.View {
	visible := p.nav.Visible(ctx, t, p.Authorize)
	sections := workspaceSections(p.at, visible, p.resources, o)
	title, description := fallback(t.Name, o.Text("admin.workspace.your_workspace", "Your workspace")), o.Text("admin.workspace.tenant_description", "Manage your team and the work happening here.")
	if t.Operator {
		title, description = o.Text("admin.workspace.overview", "Platform overview"), o.Text("admin.workspace.operator_description", "Manage tenants and access. Open a tenant workspace to work with its data.")
	}
	// The intro is the title and one sentence; FormSentence carries the reading
	// measure a full-width SectionHeader description does not.
	body := []g.Node{components.SectionHeader(components.SectionHeaderProps{Title: title, Level: 1})}
	for _, entry := range p.primary.Visible(ctx, t, p.Authorize) {
		item := components.SidebarItem{Label: entry.Label, Href: entry.Screen}
		body = append(body, components.Stack(components.StackProps{Gap: "3"},
			components.FormSentence(components.FormSentenceProps{Text: workspacePurpose(p.at, item, p.resources, o)}),
			components.Flex(components.FlexProps{Direction: "row"},
				components.Button(components.ButtonProps{Label: entry.Label, Href: entry.Screen, Variant: "primary"}))))
	}
	body = append(body, components.FormSentence(components.FormSentenceProps{Text: description}))
	if t.Operator {
		results := checks(ctx)
		if len(results) > 0 {
			message, tone := o.Text("admin.workspace.healthy", "System checks passed"), "success"
			for _, c := range results {
				if c.err != nil {
					message, tone = o.Text("admin.workspace.unhealthy", "System needs attention"), "danger"
					break
				}
			}
			body = append(body, components.Flex(components.FlexProps{Gap: "3", Align: "center", Wrap: true},
				components.Badge(components.BadgeProps{Label: message, Tone: tone, Dot: true}),
				components.Link(components.LinkProps{ComponentProps: components.ComponentProps{Attrs: screenAnchor("dashboard", "action.health")}, Href: p.at.health.at, Label: o.Text("admin.workspace.review_health", "Review health"), Variant: "text"})))
		}
	}
	if len(sections) == 0 {
		body = append(body, components.EmptyState(components.EmptyStateProps{
			Title:       o.Text("admin.workspace.no_access", "Your workspace is ready for access"),
			Description: o.Text("admin.workspace.ask_access", "Ask your administrator to give you access to the tools you need."), Bordered: true,
		}))
	}
	var areas []g.Node
	for _, section := range sections {
		var links []g.Node
		for _, item := range section.Items {
			var count g.Node
			for _, r := range p.resources {
				if r.Screen != item.Href || r.Count == nil {
					continue
				}
				if total, err := r.Count(ctx); err == nil {
					label := strconv.FormatInt(total, 10)
					if total == 0 {
						label = o.Text("screens.empty_collection", "No %s yet.", strings.ToLower(item.Label))
					}
					count = components.Badge(components.BadgeProps{Label: label, Tone: "neutral"})
				}
				break
			}
			heading := []g.Node{components.Icon(components.IconProps{Name: item.Icon}),
				components.Text(components.TextProps{Element: "span", Content: item.Label, Weight: "semibold"})}
			if count != nil {
				heading = append(heading, count)
			}
			links = append(links, components.CardWithSlots(components.CardProps{
				ComponentProps: components.ComponentProps{Class: style.New().Bg(style.ColorTransparent).Compile()},
				Href:           item.Href, Hoverable: true, Padding: "sm",
			}, components.CardSlots{
				Content: []g.Node{components.Stack(components.StackProps{Gap: "3"},
					components.Flex(components.FlexProps{Gap: "3", Align: "center", Wrap: true}, heading...),
					components.FormSentence(components.FormSentenceProps{Text: workspacePurpose(p.at, item, p.resources, o)}),
				)},
			}))
		}
		areas = append(areas, h.Section(components.Stack(components.StackProps{Gap: "3"},
			components.Heading(components.HeadingProps{Text: section.Label, Level: 2, Size: 3}),
			components.Grid(components.GridProps{Columns: "1", Gap: "3"}, links...),
		)))
	}
	body = append(body, components.Grid(components.GridProps{Columns: "1", Gap: "6"}, areas...))
	return page.View{Title: o.Text("admin.workspace.dashboard", "Dashboard"), Body: body}
}

// healthPage is the readiness probe with names on it. /ready answers 200 or 503
// and names what failed; this shows the same checks one at a time, which is
// what somebody looking at a broken deployment needs.
func healthPage(results []result, o resource.Options) page.View {
	rows := make([]components.TableRow, 0, len(results))
	for _, c := range results {
		state, tone := o.Text("admin.workspace.ok", "ok"), "success"
		if c.err != nil {
			state, tone = o.Text("admin.workspace.failing", "failing"), "danger"
		}
		rows = append(rows, components.TableRow{ID: c.name, Cells: map[string]any{
			"check": c.name, "state": state, "tone": tone,
		}})
	}
	return page.View{Title: o.Text("admin.workspace.health", "Health"), Body: []g.Node{
		components.Toolbar(components.ToolbarProps{
			Title: o.Text("admin.workspace.health", "Health"), Subtitle: o.Text("admin.workspace.health_description", "Service availability at the time this page was loaded.")}),
		components.TableWithSlots(components.TableProps{
			Columns: []components.TableColumn{
				{Key: "check", Label: o.Text("admin.workspace.check", "Check"), Primary: true},
				{Key: "state", Label: o.Text("admin.workspace.state", "State")},
			},
			Rows: rows, EmptyText: o.Text("admin.workspace.health_unknown", "Health information is unavailable. Try again or contact your operator."),
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
			Title: "Tenants", Subtitle: "Choose a workspace to manage. Each has its own people and data."}),
		components.TableWithSlots(components.TableProps{
			Columns: []components.TableColumn{
				{Key: "name", Label: "Tenant", Primary: true},
				{Key: "slug", Label: "Slug"},
				{Key: "status", Label: "Status"},
				{Key: "hosts", Label: "Served at"},
			},
			Rows:      rows,
			EmptyText: "No tenant workspaces are available.",
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
