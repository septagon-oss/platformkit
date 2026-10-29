// Package module defines what a module is.
//
// A module is a plain Go package that exports a Module value, built by its one
// constructor, New(Deps). An application composes modules with pkit,
// pkit.NewApp(name).Use(...): each declares what it provides and needs, and pkit
// resolves and checks the whole plan before anything opens. There is no init(),
// no registry addressed by string and no discovery. See docs/adr/0022.
//
// The manifest below is what the kernel needs to know about a module that it
// cannot learn from a function call: the permissions it defines, the events it
// emits and consumes, its periodic work, where it appears in navigation, what
// makes it healthy, its SQL, and its routes. Nothing else belongs in it — a
// field the kernel never reads is a field a module will fill in and no one will
// honour.
package module

import (
	"fmt"
	"github.com/septagon-oss/platformkit/kit/screen"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tour"
)

// Module is one business capability, described to the kernel.
type Module struct {
	// Name is the module's namespace: its events are prefixed with it, so an
	// event name says which module emitted it. Permissions are named after the
	// resource they guard, not after the module, because a permission outlives
	// the module that first defined it.
	Name string

	// Permissions are the permission keys this module defines, "<resource>:<action>".
	Permissions []Permission

	// Roles are the people this module is for: a role it can describe without
	// being told, because the grants it needs are its own permissions. "A role a
	// module declares opens that module's screens" is only a rule if the
	// declaration exists somewhere the kernel can read it, and before this field
	// nowhere did: the only roles in an installation were auth's two and whatever
	// the composition happened to list by hand, so nothing could check that a
	// granted role reached the screens its module serves, and an administrator
	// granting "editor" was granting a guess.
	//
	// Seeding is the composition's act, not this manifest's: apps/platformkit
	// builds auth.SeedRoles' defaults from these rows (its seedRoles), and
	// kit/app's Validate refuses a declaration that lies. A module that wants a
	// role nobody should get in every tenant declares nothing and lets the product
	// name it.
	Roles []RoleDecl

	// Events are the events this module emits, by name: "<module>.<event>",
	// inside the module's own namespace. A name is all the kernel needs to
	// refuse a route that would publish what no manifest promised, to name a
	// subscription's subject, and to expand a SubscribeAll module — so a module
	// whose payload it cannot describe (a hand-built document, a type that
	// marshals itself) still gets to emit by naming the event here.
	//
	// Every event a rest.Spec would publish has to appear in this list or in
	// Declared, or the app refuses to start: a module that emits something it
	// never promised is an integration nobody can find. Module.Emits is the one
	// list that says so; nothing reads these two fields separately.
	Events []string

	// Declared names emitted events with their payload types for outbox validation
	// and AsyncAPI. Emits prefers a typed declaration over the same bare Events
	// name; a bare event has no payload validation and is reported as uncovered.
	Declared []events.Declared

	// Subscriptions are the events this module handles. The worker role
	// subscribes each one; the name has to be an event some module emits.
	Subscriptions []events.Subscription

	// Streams are the events this module lets a live connection watch, and who
	// may. A stream is a notice and never a payload, and an event nothing streams
	// stays exactly as it was: the durable path never reads this list. See Stream.
	Streams []Stream

	// SubscribeAll says this module handles every event the application emits,
	// whichever module emits it: the kernel expands the one subscription above
	// into one per declared event, after every manifest has been read.
	//
	// There is one such module, modules/audit, and the field exists because the
	// alternative failed quietly. main used to compute the list and hand it over
	// as a dependency, which was correct only while audit was composed last: a
	// module listed after it was a module nothing recorded, and nothing said so.
	//
	// A manifest that sets it declares exactly one subscription, with no Name:
	// the name is what the kernel fills in, once per event.
	SubscribeAll bool

	// Jobs are this module's periodic work. The worker role schedules each one,
	// and exactly one instance in the cluster runs it per tick.
	Jobs []jobs.Job

	// Nav is where this module appears in navigation.
	Nav []NavEntry
	// Views are immutable custom-screen descriptors, checked again after Routes.
	Views []screen.View

	// Tours is how this module's features teach themselves: instructions
	// anchored to published screens, read by whatever renders them and
	// validated twice — here, as a declaration, and after routes are
	// registered, against what the composition actually published. A
	// composition that selects no tour runner validates them all the same and
	// shows nothing.
	Tours []tour.Tour

	// There is no Health. Nothing in three repositories ever contributed a
	// check, and /ready is the one question a probe can act on — is this
	// instance's database reachable — which kit/app asks directly. A module
	// that has a dependency of its own to check adds the field back in the
	// commit that uses it.

	// Migrations is this capability's SQL at the filesystem root. Name owns
	// its append-only history; versions are local to this module. Composition
	// order determines which capability migrates first.
	Migrations fs.FS

	// Adopts is history this module takes over from another owner: the
	// versions another owner's ledger rows carry that are now files of
	// Migrations, under the same numbers. The reference modules declare the
	// files the foundation applied before each module owned its own SQL; a
	// module that never shared an owner declares nothing. See db.Adoption.
	Adopts []db.Adoption

	// RulesFrom is the first version of Migrations the runner's rule table guards,
	// and it travels to the kernel beside the files it describes. Zero guards every
	// file, which is right for a module added after the rules existed; a module
	// whose already-applied files would be refused names the version past the last
	// of them, because a rule cannot be refused on bytes that are immutable. The
	// number is the module's fact and the module states it. See db.MigrationSource.
	RulesFrom int64

	// PersonalData is this module's claim about the person columns in the SQL it
	// owns: which table holds a person's own id, which other columns name a person,
	// which text columns an identifier may have been copied into, and which uuid
	// columns name a row and not a person — or, for a module whose tables carry no
	// such column at all, the reason that is so.
	//
	// The kernel reads it three ways, which is what makes it a declaration and not
	// a comment: Validate refuses it against this module's own migration files
	// (kit/personaldata.Check, on the bytes and with no database), it is the list
	// the per-person erasure re-reads to prove it worked, and it is what the export
	// manifest says it asked. A module that adds a person column and files nothing
	// is refused at boot rather than discovered by a person's request. See ADR 0020.
	PersonalData personaldata.Declaration

	// Routes registers this module's operations, each with its authorization,
	// on the surface the kernel built for it. The module chooses a router —
	// r.Public, r.App, r.Ops — and writes a path relative to it; the prefix, the
	// module segment and the middleware chain are the kernel's to compose and
	// its to refuse. See httpx.Surfaces.
	Routes func(r httpx.Surfaces)

	// SystemAccess declares that this module has work no tenant transaction can
	// do, and what that work is. The kernel mints one cross-tenant credential per
	// module that declares it and hands it out while that module is being wired
	// (httpx.Surfaces.SystemToken); a module that takes one without declaring it
	// is refused at composition, and one that declares it and never takes it is
	// refused too — the declaration is read in both directions. The zero value
	// says the module crosses no tenant boundary, which is the answer for every
	// module but the few this repository holds.
	//
	// It declares a scope and not an authorization: who may make the request that
	// reaches the handler holding the token is that route's own guard, and
	// docs/adr/0006 says why the two are not the same question.
	SystemAccess SystemAccess

	// Moved is the addresses this module used to answer at and where each one
	// lives now. The kernel answers an old address with a redirect and nothing
	// else — 302 for a safe method, 307 for the rest, never cached, never a
	// second mount — by the same rules as its own migration table
	// (kit/httpx/aliases.go). A module that moves a page declares a row here
	// rather than writing a handler for the old address; the surface gate
	// refuses a route mounted at an address a row moves.
	Moved []Move
}

// Move is one old address, or one old subtree, and where it lives now. Both are
// whole paths as a browser sends them ("/pets/animals", "/app/pets/animals"); a
// request under From keeps its remainder and its query.
type Move struct {
	From, To string
}

// SystemAccess is one module's declared need for a transaction that sees every
// tenant. Reason is the whole of it: one sentence, in source language, naming
// the work. It is the audit record of every cross-tenant transaction the module
// opens — a slog field and a span attribute, and nowhere else — so it names the
// work rather than the capability, and Validate refuses anything longer or
// spanning lines. It is operator text and deliberately not translatable: a
// security fact a tenant could rewrite is not a security fact.
type SystemAccess struct {
	Reason string
}

// maxSystemReason bounds the one sentence a module says about its cross-tenant
// work. The value is a log field and a span attribute and nothing else, so a
// bound is what keeps the audit line a line.
const maxSystemReason = 200

// Declared says this module crosses tenant boundaries. An empty Reason is not a
// weaker claim but no claim, which is why the question is a method and the field
// stays the zero value.
func (s SystemAccess) Declared() bool { return strings.TrimSpace(s.Reason) != "" }

// fault is why this declaration is not a usable audit sentence, or "" when it
// is. The refusal names what to write rather than only what is wrong, because
// whoever reads it is editing a manifest and not debugging a request.
func (s SystemAccess) fault() string {
	switch {
	case !s.Declared():
		return ""
	case len(s.Reason) > maxSystemReason:
		return fmt.Sprintf("SystemAccess reason is %d characters; it is the audit record of every cross-tenant transaction this module opens — name the work in one sentence of at most %d characters", len(s.Reason), maxSystemReason)
	case strings.ContainsAny(s.Reason, "\n\t"):
		return "SystemAccess reason spans lines; it is one sentence in a log field and a span attribute"
	}
	return ""
}

// Emits returns everything the module says it emits, in the one shape the
// kernel checks: each typed declaration as it stands, and each bare name in
// Events as that declaration with no payload type, which is the state
// events.Declared reserves for a payload the kernel cannot describe. A name
// given both ways is one event and appears once, as the typed declaration.
//
// Validate, Expand, the coverage line and the AsyncAPI document all read this
// method rather than either field, so the two spellings of a manifest's event
// list cannot disagree about what the composition emits.
func (m Module) Emits() []events.Declared {
	if len(m.Events) == 0 {
		return m.Declared
	}
	typed := make(map[string]bool, len(m.Declared))
	for _, d := range m.Declared {
		typed[d.Name] = true
	}
	out := make([]events.Declared, 0, len(m.Declared)+len(m.Events))
	for _, name := range m.Events {
		if !typed[name] {
			out = append(out, events.Declared{Name: name})
		}
	}
	return append(out, m.Declared...)
}

// Permission is one thing a role can be granted.
type Permission struct {
	Key string

	// Label is the grant in the words of the module that defines it: "read
	// tasks", "manage roles". It is source-language text, the readable fallback
	// handed to the same Locale.Text(key, fallback) seam every other label in
	// this kernel uses, and the tenant's language for it lives under
	// `permission.<key>` in the module's own catalogue file.
	//
	// It exists because a refusal has to tell a person what they lack, and a
	// key does not: "AUTH_DENIED: this operation requires task:read" names a
	// token and leaves the reader to guess whether guessing is safe. Validate
	// refuses a permission with no label, and one labelled with its own key, so
	// the field cannot be filled by copying the thing it explains.
	Label string

	// Operator says this permission belongs to the installation rather than to
	// a customer: only the operator's own tenant may exercise it at all, and no
	// wildcard satisfies it — a role has to name it.
	//
	// It is a field on the manifest as well as a route declaration
	// (httpx.OperatorPermission) because the two have to agree, and a check
	// needs both sides to read. kit/app refuses to start when a route and the
	// manifest that defines its permission disagree, naming both: a control
	// plane route that declared the ordinary kind would be reachable by every
	// customer's administrator, and an ordinary route that declared this one
	// would be reachable by nobody but the operator.
	Operator bool
}

// Grants returns declared permissions and labels in composition order for
// request guards and role seeding. Validate must first reject malformed or
// duplicate permissions; this function does not validate them again.
func Grants(mods []Module) []tenancy.Grant {
	var out []tenancy.Grant
	for _, m := range mods {
		for _, p := range m.Permissions {
			out = append(out, tenancy.Grant{Permission: p.Key, Label: p.Label, Operator: p.Operator})
		}
	}
	return out
}

// RoleDecl is one role a module declares for its own capability: seeded into
// every new tenant by the composition, and granting nothing but permissions this
// module's own manifest defines. It carries no client, sector, jurisdiction or
// price: the shape of the grant is the whole of what a module may say about a
// person, and which of its roles a product composes is the product's list.
type RoleDecl struct {
	// Name is the role as it appears in the tenant's role table and on the
	// roles screen: a lower-case identifier, at most auth's MaxRoleName
	// characters, and not one of the two names auth owns (admin, member), which
	// Validate refuses rather than letting a module shadow.
	Name string

	// Grants are the permission keys this role holds, each one declared by this
	// module and none of them an operator's. An empty list is a role that grants
	// nothing — the shape auth's own member role holds by design, and the reason
	// a module may not declare one.
	Grants []string
}

// NavEntry is one link in the application's navigation, shown to a caller who
// holds Permission. There is no Order: nav is rendered in composition order,
// which is the order main lists the modules in, and a second ordering nothing
// reads is a number every module would guess at.
//
// Screen names the workspace screen the entry leads to as the module and the
// entity — "task/tasks" — and not as a URL. The old field was Path, and nine
// manifests wrote "/admin/task/tasks" into it, which meant a module named the
// surface its navigation lived on and every shell move was nine edits. The
// workspace's own address is the kernel's to know (httpx.Workspace), and the
// rename is what made every one of those nine a compile error rather than a
// string that still resolves until somebody moves the shell.
type NavEntry struct {
	Label string
	// Screen is "<module>/<entity>", relative to the workspace: "task/tasks".
	Screen     string
	Permission string
}

// moduleName is the grammar of a module name. It is the prefix of every event
// the module emits, so it has to be an identifier.
var moduleName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// screenName is the grammar of a nav entry's Screen: the module and the entity,
// one slash, no leading slash and no prefix. It is the shape the generated
// screens are mounted at, which is the only reason a nav entry can name a
// screen without naming where the shell lives.
var screenName = regexp.MustCompile(`^[a-z][a-z0-9_]*(/[a-z][a-z0-9_]*)*$`)

// roleName is auth's ValidRoleName grammar restated, because kit/module may not
// import a module's contracts and a name that fails the module's check would
// fail the seed's write later, with a worse message. The two are kept in step by
// the case in kit/module's tests that names auth's constant.
var roleName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// maxRoleName is auth.MaxRoleName, for the same reason roleName restates its
// grammar: the column the seed writes into is bounded by it.
const maxRoleName = 64

// reservedRoleNames are the two roles auth owns and seeds itself in every
// tenant: the administrator and the empty member. A module that declared either
// would be declaring a grant into a row auth has already written, and ON
// CONFLICT DO NOTHING would make the lie silent.
var reservedRoleNames = map[string]bool{"admin": true, "member": true}

// surfacesNames are the words that belong to the kernel: a module called
// "public", "ops" or "app" would compose a route whose prefix and its surface
// disagree, and the mount address would read as two things at once. "app" is the
// composition's own namespace at /api/v1/app, which is why no module may take it.
var surfacesNames = map[string]bool{"public": true, "ops": true, "app": true}

// Validate checks a set of modules against the rules that make the namespacing
// real: names are unique and well-formed, no two modules define the same
// permission, every nav entry points at a permission somebody declared, every
// event a module emits is namespaced by that module's name, every subscription
// names an event somebody emits, and every job has a schedule that parses.
//
// It reports every violation in one error rather than the first, because a
// composition is fixed once and the whole list is what a person needs.
// validMoves refuses a row the redirect could not honour: a path that is not
// absolute, a row that points at itself, or an old address two rows claim.
func validMoves(mods []Module) []string {
	var bad []string
	from := map[string]string{}
	for _, m := range mods {
		for _, mv := range m.Moved {
			switch {
			case !strings.HasPrefix(mv.From, "/") || !strings.HasPrefix(mv.To, "/"):
				bad = append(bad, fmt.Sprintf("module %q moves %q to %q; both must be absolute paths", m.Name, mv.From, mv.To))
			case strings.TrimSuffix(mv.From, "/") == strings.TrimSuffix(mv.To, "/"):
				bad = append(bad, fmt.Sprintf("module %q moves %q to itself", m.Name, mv.From))
			case from[mv.From] != "":
				bad = append(bad, fmt.Sprintf("modules %q and %q both move %q", from[mv.From], m.Name, mv.From))
			default:
				from[mv.From] = m.Name
			}
		}
	}
	return bad
}

// security.access_requested is the ask that follows a refusal, emitted for the
// same reason: no module ran, the person was refused by the kernel's own guard.
//
// KernelEvents are the events the kernel itself emits, which no module's manifest
// can declare because no module raises them: security.denied is published by kit/app
// for every attributable refused authorization (see kit/httpx Options.Denied). They
// count as emitted for Validate and are expanded for a SubscribeAll module, so
// modules/audit keeps them like any other event.
//
// Deprecated: read KernelEventNames(), which hands out a copy a caller cannot write
// into. This slice is the same storage every composition in the process reads, and one
// append that grew it past its length would change what the kernel emits for all of them.
var KernelEvents = []string{"security.denied", "security.access_requested", events.EventOperationOutcome}

// KernelEventNames is KernelEvents as a copy: the names the kernel emits itself, which
// no module may declare and which a SubscribeAll module subscribes to beside the
// composition's own. It is a fresh slice per call, so a caller that sorts, truncates or
// appends to the answer leaves the kernel's own list where it found it.
func KernelEventNames() []string {
	return slices.Clone(KernelEvents)
}

// KernelName is the manifest name kit/app gives the kernel's own share of the
// composition. It is named because it is read by a rule: KernelEvents are the
// only events that may be declared outside the emitting manifest's namespace
// (Validate), and saying which manifest is the kernel's is the other half of
// that sentence.
const KernelName = "platformkit"

// Expand copies modules and expands SubscribeAll into sorted subscriptions
// for all composed and kernel events, then clears SubscribeAll. Call before
// Validate. Other declarations are unchanged and input slices are not mutated.
func Expand(mods []Module) []Module {
	seen := map[string]bool{}
	var all []string
	for _, e := range KernelEventNames() {
		if !seen[e] {
			seen[e], all = true, append(all, e)
		}
	}
	for _, m := range mods {
		for _, e := range m.Emits() {
			if !seen[e.Name] {
				seen[e.Name], all = true, append(all, e.Name)
			}
		}
	}
	sort.Strings(all)

	out := slices.Clone(mods)
	for i, m := range out {
		if m.SubscribeAll && len(m.Subscriptions) == 1 {
			template := m.Subscriptions[0]
			subs := make([]events.Subscription, 0, len(all))
			for _, e := range all {
				// App travels with the template rather than being invented here: the
				// expansion is one subscription per emitted event and nothing else,
				// and the durable each of them gets is the template's app joined to
				// that event (kit/appname.Durable). Dropping the field would hand a
				// SubscribeAll module the unscoped consumer name for every event in
				// the system.
				subs = append(subs, events.Subscription{App: template.App, Module: template.Module, Name: e, Handler: template.Handler})
			}
			m.Subscriptions, m.SubscribeAll = subs, false
			out[i].SubscribeAll = false
		}
		// The two facts the kernel can answer for a manifest: a subscription no
		// module is named on belongs to the manifest that carries it, and a screen
		// named by its entity alone is this module's own screen. Both are the same
		// answer Validate gives, filled where it is the only moment before either
		// is read — pkit, the durable consumer name and the navigation are all
		// asked long before any router exists.
		out[i].Subscriptions = attributedTo(m.Subscriptions, m.Name)
		out[i].Nav = screensOf(m.Nav, m.Name)
	}
	return out
}

// attributedTo names every subscription that names none, and only those: a name
// the manifest already wrote survives, including another module's, because that
// disagreement is what Validate refuses and the durable consumer name is stamped
// from it. The answer is always a slice of its own, so the expanded plan holds no
// window onto the manifest it was built from.
func attributedTo(subs []events.Subscription, owner string) []events.Subscription {
	out := make([]events.Subscription, len(subs))
	for i, s := range subs {
		if s.Module == "" {
			s.Module = owner
		}
		out[i] = s
	}
	return out
}

// screensOf completes the nav entries that name only their entity: "tasks" in
// module task is the task module's own screen, "task/tasks". An entry that
// already names a module keeps it, including across modules — a shell linking to
// another module's screens says so — and an empty Screen is left for Validate
// to refuse, which is what makes filling it and requiring it one rule. Like
// attributedTo it answers with a slice of its own.
func screensOf(entries []NavEntry, owner string) []NavEntry {
	out := make([]NavEntry, len(entries))
	for i, n := range entries {
		if n.Screen != "" && !strings.Contains(n.Screen, "/") {
			n.Screen = owner + "/" + n.Screen
		}
		out[i] = n
	}
	return out
}
