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

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/health"
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

	// Events are the event names this module emits, "<name>.<event>". Every
	// event a rest.Spec would publish has to appear here, or the app refuses to
	// start: a module that emits something it never promised is an integration
	// nobody can find.
	Events []string

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

	// Health are the checks that must pass before an instance serves.
	Health []health.Check

	// Migrations is this module's SQL, with the files at the root of the given
	// fs.FS. Every module in this repository leaves it nil and puts its SQL in
	// migrations/ instead, which is where ARCHITECTURE.md says SQL lives; the
	// field is the door for a module that ships its own, and the files it
	// carries are still numbered uniquely across the whole application, because
	// there is one ledger. app.Run refuses a collision.
	Migrations fs.FS

	// Routes registers this module's operations, each with its authorization.
	Routes func(api *httpx.API)
}

// Permission is one thing a role can be granted.
type Permission struct {
	Key string

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

// NavEntry is one link in the application's navigation, shown to a caller who
// holds Permission. There is no Order: nav is rendered in composition order,
// which is the order main lists the modules in, and a second ordering nothing
// reads is a number every module would guess at.
type NavEntry struct {
	Label      string
	Path       string
	Permission string
}

// moduleName is the grammar of a module name. It is the prefix of every event
// the module emits, so it has to be an identifier.
var moduleName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Validate checks a set of modules against the rules that make the namespacing
// real: names are unique and well-formed, no two modules define the same
// permission, every nav entry points at a permission somebody declared, every
// event a module emits is namespaced by that module's name, every subscription
// names an event somebody emits, and every job has a schedule that parses.
//
// It reports every violation in one error rather than the first, because a
// composition is fixed once and the whole list is what a person needs.
func Validate(mods []Module) error {
	var bad []string
	add := func(format string, args ...any) { bad = append(bad, fmt.Sprintf(format, args...)) }

	names := map[string]bool{}
	owner := map[string]string{} // permission key -> the module that defined it
	for _, m := range mods {
		switch {
		case m.Name == "":
			add("a module has no name")
			continue
		case !moduleName.MatchString(m.Name):
			add("module %q: a name is a lower-case identifier, because the events it emits are prefixed with it", m.Name)
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
			if first, seen := owner[p.Key]; seen {
				add("module %q: permission %q is already defined by module %q", m.Name, p.Key, first)
				continue
			}
			owner[p.Key] = m.Name
		}

		for i, c := range m.Health {
			if c == nil {
				add("module %q: health check %d is nil; /ready would panic on it", m.Name, i)
			}
		}

		for _, j := range m.Jobs {
			if err := jobs.Valid(j); err != nil {
				add("module %q: %s", m.Name, err)
			}
		}

		for _, e := range m.Events {
			if !events.ValidName(e) {
				add("module %q: event %q is not %q", m.Name, e, "<name>.<event>")
				continue
			}
			if !strings.HasPrefix(e, m.Name+".") {
				add("module %q: event %q is not namespaced by the module that emits it", m.Name, e)
			}
		}
	}

	// Nav and subscriptions are checked after every module has been read,
	// because both point at something another module owns: a link is about what
	// the reader may see, and a subscription is about what somebody else emits.
	emitted := map[string]bool{}
	for _, m := range mods {
		for _, e := range m.Events {
			emitted[e] = true
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
			if n.Permission == "" {
				add("module %q: nav entry %q declares no permission; a link everyone sees is still a decision", m.Name, n.Label)
				continue
			}
			if _, ok := owner[n.Permission]; !ok {
				add("module %q: nav entry %q requires permission %q, which no module defines", m.Name, n.Label, n.Permission)
			}
		}
	}

	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return errors.New("module: invalid composition:\n  " + strings.Join(bad, "\n  "))
}

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
	for _, m := range mods {
		for _, e := range m.Events {
			if !seen[e] {
				seen[e], all = true, append(all, e)
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
			subs = append(subs, events.Subscription{Module: template.Module, Name: e, Handler: template.Handler})
		}
		out[i].Subscriptions, out[i].SubscribeAll = subs, false
	}
	return out
}
