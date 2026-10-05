package pkit

// The customisation methods: the four facts a kernel module cannot decide and
// one page an application owns (decision 0074 rule 2). Each is one word for one
// decision, records it, and returns the app. None of them answers anything —
// Build does — and every one of them is read: by Build's refusal over the
// recorded roles, by the engine's own options, or by the Skin the app's
// renderers are built with.
//
// They are methods rather than fields of a struct somebody passes in because the
// sentence is the point: `NewApp("collect").Use(…).Theme(…).Home(…)`. They are
// not options functions with errors because a chain that could fail at each word
// is a chain whose author checks each return.

import (
	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/module"
)

// Catalogue is the copy an application is worded in: the one value the refusal
// page and every shell render from. Naming the interface here rather than
// importing a locale provider keeps xtext — or any other — out of pkit.
//
// It carries only what pkit reads. The set of languages a tenant may be served
// in is a fact a tenant records, and the application that owns the catalogue
// reads it off its own value (apps/platformkit/modules.go hands it to the tenant
// module); pkit neither stores that list nor pretends to.
type Catalogue interface {
	locale.Messages
}

// Theme is the pair of palettes the application's own chrome is drawn with.
// design.Pair is the foundation's value: everything above the tokens is written
// in terms of a role, and this is the one line that changes when a client's
// colours do.
func (a *App) Theme(pair design.Pair) *App {
	a.themes++
	a.theme = pair
	return a
}

// Home is the way on a refusal offers: the address this application's front door
// is at, and the one link the failure page points at. Every Home named is
// recorded, because the refusal belongs to Build and a method that refused the
// second call would be a chain that validates.
func (a *App) Home(path string) *App {
	a.homes = append(a.homes, path)
	return a
}

// Languages is the application's copy: the one value the refusal page and every
// shell are worded from, because the words and the languages are the same files.
// The languages a tenant may be served in are the catalogue's own reading, which
// the application hands to the module that records it — see Catalogue.
func (a *App) Languages(catalog Catalogue) *App {
	a.copies++
	a.copy = catalog
	return a
}

// Role is one role this application says a new tenant begins as: its name and the
// permissions it holds. See Roles for what Build does with it.
//
// Permissions are named by key only. The module that defines a key owns its
// words and its operator flag, which is why Build reads both off the built
// manifest rather than storing a second copy here — a role spelled with an
// operator grant is refused, and a role spelled with a permission nobody
// composes is refused, both from the manifests that decide it.
type Role struct {
	Name   string
	Grants []string
}

// Roles names the roles this application says a tenant begins as: their names and
// what they hold. Who may join a tenant afterwards stays the auth module's
// command; this is the application's own claim about who it is for.
//
// What Build does with the claim is two things, and no more than the two: it
// refuses a grant no composed module defines and an operator grant a tenant's
// role may not hold, both read off the built manifests, and it prints the roles
// in Explain, so the composition file says what a tenant of this application
// starts as. Seeding is not here — no engine seam hands a role to a tenant at
// creation — and the application that seeds does it through the tenant module's
// own creation hooks, as apps/platformkit's seedRoles does.
func (a *App) Roles(roles ...Role) *App {
	a.roles = append(a.roles, roles...)
	return a
}

// ErrorPage sets the document a browser sees for any refusal, 404 included.
//
// It is a build function rather than an httpx.Fault because the page needs what
// only the resolved composition knows — the words the defining module chose for
// the permission it refused, the front door it offers — and those exist only
// after the modules are built. It runs once, in Build's plan phase, and it is
// the app's own chrome: kit may not import ui, which is the whole reason the
// kernel leaves this seam open rather than shipping a page of its own. An app
// that sets nothing leaves every refusal as problem+json, which is what an API
// client wants and what every application did before the field existed.
func (a *App) ErrorPage(build func(Skin) httpx.Fault) *App {
	a.refusals++
	a.refusal = build
	return a
}

// AskForAccess is the browser's half of asking for what a refusal refused: the
// reach — who to tell, and how — and the form that asks. Both or neither: a
// reach with no page is a refusal that offers nothing, and a page with no reach
// is a button that lies.
//
// The two doors are one method because they are one decision with two owners:
// the composition knows who holds role management in this tenant, and ui owns
// the page. The command behind both is the kernel's.
func (a *App) AskForAccess(ask httpx.AskForAccess, form func(router *httpx.Router)) *App {
	a.ask = ask
	a.askPage = form
	return a
}

// WorkspaceCatalog mounts the document a native shell reads to know what this
// application has. The kernel owns the address and ui renders the body, and only
// the application can write the line that joins them — which is why it is here
// and not a call kit/app makes: a composition with resources and no renderer is
// refused by the engine, and the renderer is chrome.
func (a *App) WorkspaceCatalog(mount func(api *httpx.API)) *App {
	a.catalog = mount
	return a
}

// Skin is what the application recorded, plus the words the composed manifests
// chose for the permissions they name. Build hands it to the app's own renderers
// once the composition is resolved, which is what makes a refusal page allowed to
// name a grant: it reads the grant's name off the module that defines it.
type Skin struct {
	Theme design.Pair
	Home  string
	Copy  Catalogue
	mods  []module.Module
}

// Label is the words the module that defines a permission chose for it, and ""
// when no composed module defines it. A refusal may name a grant; what it may not
// do is invent the grant's name.
func (s Skin) Label(permission string) string {
	for _, m := range s.mods {
		for _, p := range m.Permissions {
			if p.Key == permission {
				return p.Label
			}
		}
	}
	return ""
}
