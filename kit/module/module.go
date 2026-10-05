// Package module defines what a module is.
//
// A module is a plain Go package that exports a Module value. main constructs
// each one with its typed dependencies, in dependency order, and passes the
// list to app.New. There is no registry to enrol in, no group to join and no
// reflection to resolve: the wiring graph is the argument list, and the
// compiler checks it. See docs/adr/0002.
//
// The manifest below is what the kernel needs to know about a module that it
// cannot learn from a function call: the permissions it defines, the events it
// emits and consumes, its periodic work, where it appears in navigation, what
// makes it healthy, its SQL, and its routes. Nothing else belongs in it — a
// field the kernel never reads is a field a module will fill in and no one will
// honour.
package module

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
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

	// Declared are the events this module emits, each named with the Go type of
	// its payload: events.Declare[contracts.Invited](contracts.EventInvited).
	// It is the typed form of Events, and the one a module composes from this
	// repository's own contracts uses.
	//
	// The payload type is not decoration. It is projected into a JSON Schema
	// (kit/events/schema.go), the outbox refuses a payload that is not one
	// before the row is written, and the composition's AsyncAPI document is
	// emitted from it (kit/app/asyncapi.go). An event named only in Events is
	// the same list item with Payload nil, which is what events.Declared calls
	// a payload the kernel does not describe: published unchecked, listed as
	// uncovered rather than pretended to be.
	//
	// A name given in both fields is one event, described — Emits keeps the
	// typed item and drops the bare name.
	Declared []events.Declared

	// Subscriptions are the events this module handles. The worker role
	// subscribes each one; the name has to be an event some module emits.
	Subscriptions []events.Subscription

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

	// Routes registers this module's operations, each with its authorization,
	// on the surface the kernel built for it. The module chooses a router —
	// r.Public, r.App, r.Ops — and writes a path relative to it; the prefix, the
	// module segment and the middleware chain are the kernel's to compose and
	// its to refuse. See httpx.Surfaces.
	Routes func(r httpx.Surfaces)

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

func Validate(mods []Module) error {
	var bad []string
	add := func(format string, args ...any) { bad = append(bad, fmt.Sprintf(format, args...)) }

	names := map[string]bool{}
	owner := map[string]string{}     // permission key -> the module that defined it
	roleOwner := map[string]string{} // role name -> the module that declared it
	for _, m := range mods {
		switch {
		case m.Name == "":
			add("a module has no name")
			continue
		case !moduleName.MatchString(m.Name):
			add("module %q: a name is a lower-case identifier, because the events it emits are prefixed with it", m.Name)
		case surfacesNames[m.Name]:
			add("module %q: a module may not be named public or ops; the two name surfaces", m.Name)
		}
		if names[m.Name] {
			add("module %q: declared twice", m.Name)
		}
		names[m.Name] = true

		for _, p := range m.Permissions {
			if !httpx.ValidPermission(p.Key) {
				add("module %q: permission %q is not %q", m.Name, p.Key, "<resource>:<action>")
				continue
			}
			// A label nothing would read is a field that should not exist; a label
			// equal to the key is the defect the field exists to close, and it is
			// shippable by accident — the key copied twice — unless boot says no.
			switch p.Label {
			case "":
				add("module %q: permission %q has no Label; a refusal has to name what is missing in words, not only as a key", m.Name, p.Key)
			case p.Key:
				add("module %q: permission %q is labelled with its own key; a Label is the grant in words", m.Name, p.Key)
			}
			if first, seen := owner[p.Key]; seen {
				add("module %q: permission %q is already defined by module %q", m.Name, p.Key, first)
				continue
			}
			owner[p.Key] = m.Name
		}

		for _, j := range m.Jobs {
			if err := jobs.Valid(j); err != nil {
				add("module %q: %s", m.Name, err)
			}
		}

		// A declared role is a promise about this module's own permissions, so every
		// check but one is local to it. The refusals are all compose-time, which is
		// where checkPersonas already put a bad persona grant: boot and bootstrap fail
		// before the database opens, and a role that seeds a lie never reaches a tenant.
		own := map[string]Permission{}
		for _, p := range m.Permissions {
			own[p.Key] = p
		}
		seenRole := map[string]bool{}
		for _, rd := range m.Roles {
			name := strings.ToLower(strings.TrimSpace(rd.Name))
			switch {
			case name == "":
				add("module %q declares a role with no name", m.Name)
			case !roleName.MatchString(name):
				add("module %q: role %q is not a lower-case identifier, which is what the roles table and the roles route both require", m.Name, rd.Name)
			case len(name) > maxRoleName:
				add("module %q: role %q is longer than %d characters", m.Name, name, maxRoleName)
			case reservedRoleNames[name]:
				add("module %q: role %q is auth's own to seed; a module may not declare it", m.Name, name)
			case seenRole[name]:
				add("module %q: role %q is declared twice", m.Name, name)
			case roleOwner[name] != "":
				add("module %q: role %q is already declared by module %q; two modules seeding one name would write two answers into one row", m.Name, name, roleOwner[name])
			default:
				seenRole[name], roleOwner[name] = true, m.Name
			}
			if len(rd.Grants) == 0 {
				add("module %q: role %q grants nothing; a role nobody asked for is auth's member role and that one is seeded already", m.Name, name)
			}
			for _, g := range rd.Grants {
				p, declared := own[g]
				switch {
				case !declared:
					add("module %q: role %q grants %q, which this module does not declare; a module may only open what it owns",
						m.Name, name, g)
				case p.Operator:
					add("module %q: role %q grants %q, an operator permission; a customer's role may not reach the control plane",
						m.Name, name, g)
				}
			}
		}

		// The files an adoption names are checked by db.Migrate against the
		// module's SQL before it connects; what only the manifest can say is
		// that there is SQL to check against at all.
		if len(m.Adopts) > 0 && m.Migrations == nil {
			add("module %q: adopts migration history and declares no Migrations", m.Name)
		}

		for _, e := range m.Emits() {
			if !events.ValidName(e.Name) {
				add("module %q: event %q is not %q", m.Name, e.Name, "<name>.<event>")
				continue
			}
			// The kernel's own manifest is the one exemption, and only for the
			// events KernelEvents names: security.denied is raised by the kernel
			// because the refusal is its — no module ran — so it carries the
			// kernel's namespace and not the manifest's. Every other manifest
			// still emits inside its own name, which is what makes a duplicate
			// emitter impossible rather than merely discourled.
			if !strings.HasPrefix(e.Name, m.Name+".") &&
				!(m.Name == KernelName && slices.Contains(KernelEvents, e.Name)) {
				add("module %q: event %q is not namespaced by the module that emits it", m.Name, e.Name)
			}
		}
	}

	// Nav and subscriptions are checked after every module has been read,
	// because both point at something another module owns: a link is about what
	// the reader may see, and a subscription is about what somebody else emits.
	emitted := map[string]bool{}
	for _, e := range KernelEvents {
		emitted[e] = true
	}
	for _, m := range mods {
		for _, e := range m.Emits() {
			emitted[e.Name] = true
		}
	}
	for _, m := range mods {
		// The handler once, with no name: a second subscription beside it would
		// be one the expansion silently ignored.
		if m.SubscribeAll && len(m.Subscriptions) != 1 {
			add("module %q: SubscribeAll declares %d subscriptions; it takes exactly one, the handler every event goes to",
				m.Name, len(m.Subscriptions))
		}
		for _, s := range m.Subscriptions {
			switch {
			case s.Handler == nil:
				add("module %q: subscription to %q has no handler", m.Name, s.Name)
			case s.Module != m.Name:
				add("module %q: subscription to %q is attributed to module %q", m.Name, s.Name, s.Module)
			case m.SubscribeAll && s.Name == "":
				// Unexpanded, which is a composition nobody ran through
				// Expand. Validate is called by kit/app, which expands first.
			case !emitted[s.Name]:
				add("module %q: subscribes to %q, which no module emits", m.Name, s.Name)
			}
		}
		for _, n := range m.Nav {
			if !screenName.MatchString(n.Screen) {
				add("module %q: nav entry %q names Screen %q; a nav entry names its screen relative to the workspace — %q", m.Name, n.Label, n.Screen, "task/tasks")
			}
			if n.Permission == "" {
				add("module %q: nav entry %q declares no permission; a link everyone sees is still a decision", m.Name, n.Label)
				continue
			}
			if _, ok := owner[n.Permission]; !ok {
				add("module %q: nav entry %q requires permission %q, which no module defines", m.Name, n.Label, n.Permission)
			}
		}
	}

	bad = append(bad, validMoves(mods)...)
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return errors.New("module: invalid composition:\n  " + strings.Join(bad, "\n  "))
}

// security.access_requested is the ask that follows a refusal, emitted for the
// same reason: no module ran, the person was refused by the kernel's own guard.
//
// KernelEvents are the events the kernel itself emits, which no module's manifest
// can declare because no module raises them: security.denied is published by kit/app
// for every attributable refused authorization (see kit/httpx Options.Denied). They
// count as emitted for Validate and are expanded for a SubscribeAll module, so
// modules/audit keeps them like any other event.
var KernelEvents = []string{"security.denied", "security.access_requested"}

// KernelName is the manifest name kit/app gives the kernel's own share of the
// composition. It is named because it is read by a rule: KernelEvents are the
// only events that may be declared outside the emitting manifest's namespace
// (Validate), and saying which manifest is the kernel's is the other half of
// that sentence.
const KernelName = "platformkit"

// Expand turns every SubscribeAll manifest's one subscription into one per
// event the composition emits, and returns the modules with that done.
//
// One subscription per event rather than one wildcard, because the kernel's
// durable consumers are named after the subscription: a wildcard would be one
// consumer whose backlog is every event in the system, and one slow payload
// would hold up the trail of everything else.
//
// It runs before Validate and before anything is constructed, so a module that
// arrives after the subscriber in the list is still subscribed to. The argument
// is not modified: the returned slice is a copy, sorted so every replica
// registers the same consumers, and in it SubscribeAll is cleared — the flag is
// a request and this is it answered.
func Expand(mods []Module) []Module {
	seen := map[string]bool{}
	var all []string
	for _, e := range KernelEvents {
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
		if !m.SubscribeAll || len(m.Subscriptions) != 1 {
			continue
		}
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
		out[i].Subscriptions, out[i].SubscribeAll = subs, false
	}
	return out
}
