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
// emits, where it appears in navigation, what makes it healthy, its SQL, and
// its routes. Nothing else belongs in it — a field the kernel never reads is a
// field a module will fill in and no one will honour, which is why scheduled
// work arrives here with kit/jobs and not before.
package module

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
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

	// Events are the event names this module emits, "<name>.<event>".
	Events []string

	// Nav is where this module appears in navigation.
	Nav []NavEntry

	// Health are the checks that must pass before an instance serves.
	Health []health.Check

	// Migrations is this module's SQL, with the files at the root of the given
	// fs.FS. It is optional and temporary: E2 merges every module's SQL into
	// migrations/, which is where ARCHITECTURE.md says it lives. Until then the
	// files must still be numbered uniquely across the whole application,
	// because there is one ledger; app.Run refuses a collision.
	Migrations fs.FS

	// Routes registers this module's operations, each with its authorization.
	Routes func(api *httpx.API)
}

// Permission is one thing a role can be granted.
type Permission struct {
	Key         string
	Description string
}

// NavEntry is one link in the application's navigation, shown to a caller who
// holds Permission.
type NavEntry struct {
	Label      string
	Path       string
	Permission string
	Order      int
}

// moduleName is the grammar of a module name. It is the prefix of every event
// the module emits, so it has to be an identifier.
var moduleName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// eventName is the grammar of an event: the module's name, a dot, and a
// lower-case path.
var eventName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// Validate checks a set of modules against the rules that make the namespacing
// real: names are unique and well-formed, no two modules define the same
// permission, every nav entry points at a permission somebody declared, and
// every event a module emits is namespaced by that module's name.
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

		for _, e := range m.Events {
			if !eventName.MatchString(e) {
				add("module %q: event %q is not %q", m.Name, e, "<name>.<event>")
				continue
			}
			if !strings.HasPrefix(e, m.Name+".") {
				add("module %q: event %q is not namespaced by the module that emits it", m.Name, e)
			}
		}
	}

	// Nav is checked after every module has been read, so an entry may point at
	// a permission another module defines: a link is about what the reader may
	// see, not about who owns the permission.
	for _, m := range mods {
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
