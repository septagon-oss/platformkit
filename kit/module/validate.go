package module

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
)

// Validate reports all manifest violations together, sorted by diagnostic text.
func Validate(mods []Module) error {
	v := validation{
		names:     map[string]bool{},
		owner:     map[string]string{},
		roleOwner: map[string]string{},
	}
	for _, m := range mods {
		v.module(m)
	}
	v.references(mods)
	v.bad = append(v.bad, validMoves(mods)...)
	if len(v.bad) == 0 {
		return nil
	}
	sort.Strings(v.bad)
	return errors.New("module: invalid composition:\n  " + strings.Join(v.bad, "\n  "))
}

type validation struct {
	bad       []string
	names     map[string]bool
	owner     map[string]string
	roleOwner map[string]string
}

func (v *validation) add(format string, args ...any) {
	v.bad = append(v.bad, fmt.Sprintf(format, args...))
}

func (v *validation) module(m Module) {
	if err := validateViews(m); err != nil {
		v.add("%v", err)
	}
	switch {
	case m.Name == "":
		v.add("a module has no name")
		return
	case !moduleName.MatchString(m.Name):
		v.add("module %q: a name is a lower-case identifier, because the events it emits are prefixed with it", m.Name)
	case surfacesNames[m.Name]:
		v.add("module %q: a module may not be named public or ops; the two name surfaces", m.Name)
	}
	if v.names[m.Name] {
		v.add("module %q: declared twice", m.Name)
	}
	v.names[m.Name] = true

	v.permissions(m)
	v.jobsAndData(m)
	v.roles(m)
	v.events(m)
}

func (v *validation) permissions(m Module) {
	for _, p := range m.Permissions {
		if !httpx.ValidPermission(p.Key) {
			v.add("module %q: permission %q is not %q", m.Name, p.Key, "<resource>:<action>")
			continue
		}
		// A label nothing would read is a field that should not exist; a label
		// equal to the key is the defect the field exists to close, and it is
		// shippable by accident — the key copied twice — unless boot says no.
		switch p.Label {
		case "":
			v.add("module %q: permission %q has no Label; a refusal has to name what is missing in words, not only as a key", m.Name, p.Key)
		case p.Key:
			v.add("module %q: permission %q is labelled with its own key; a Label is the grant in words", m.Name, p.Key)
		}
		if first, seen := v.owner[p.Key]; seen {
			v.add("module %q: permission %q is already defined by module %q", m.Name, p.Key, first)
			continue
		}
		v.owner[p.Key] = m.Name
	}
}

func (v *validation) jobsAndData(m Module) {
	for _, j := range m.Jobs {
		if err := jobs.Valid(j); err != nil {
			v.add("module %q: %s", m.Name, err)
		}
	}

	// Only modules with migrations can own personal-data tables. Check their

	// claims against migration bytes before opening a database; see ADR 0020.
	if m.Migrations != nil {
		for _, problem := range m.PersonalData.Check(m.Name, m.Migrations) {
			v.add("%s", problem)
		}
	}
}

func (v *validation) roles(m Module) {
	// Roles may grant only their module’s tenant permissions; validate before
	// bootstrap can persist a role that promises other access.
	own := map[string]Permission{}
	for _, p := range m.Permissions {
		own[p.Key] = p
	}
	seenRole := map[string]bool{}
	for _, rd := range m.Roles {
		name := strings.ToLower(strings.TrimSpace(rd.Name))
		switch {
		case name == "":
			v.add("module %q declares a role with no name", m.Name)
		case !roleName.MatchString(name):
			v.add("module %q: role %q is not a lower-case identifier, which is what the roles table and the roles route both require", m.Name, rd.Name)
		case len(name) > maxRoleName:
			v.add("module %q: role %q is longer than %d characters", m.Name, name, maxRoleName)
		case reservedRoleNames[name]:
			v.add("module %q: role %q is auth's own to seed; a module may not declare it", m.Name, name)
		case seenRole[name]:
			v.add("module %q: role %q is declared twice", m.Name, name)
		case v.roleOwner[name] != "":
			v.add("module %q: role %q is already declared by module %q; two modules seeding one name would write two answers into one row", m.Name, name, v.roleOwner[name])
		default:
			seenRole[name], v.roleOwner[name] = true, m.Name
		}
		if len(rd.Grants) == 0 {
			v.add("module %q: role %q grants nothing; a role nobody asked for is auth's member role and that one is seeded already", m.Name, name)
		}
		for _, g := range rd.Grants {
			p, declared := own[g]
			switch {
			case !declared:
				v.add("module %q: role %q grants %q, which this module does not declare; a module may only open what it owns",
					m.Name, name, g)
			case p.Operator:
				v.add("module %q: role %q grants %q, an operator permission; a customer's role may not reach the control plane",
					m.Name, name, g)
			}
		}
	}
}

func (v *validation) events(m Module) {
	// The files an adoption names are checked by db.Migrate against the
	// module's SQL before it connects; what only the manifest can say is
	// that there is SQL to check against at all.
	if len(m.Adopts) > 0 && m.Migrations == nil {
		v.add("module %q: adopts migration history and declares no Migrations", m.Name)
	}

	for _, e := range m.Emits() {
		if !events.ValidName(e.Name) {
			v.add("module %q: event %q is not %q", m.Name, e.Name, "<name>.<event>")
			continue
		}
		// Only the kernel manifest may emit its explicitly named cross-cutting
		// events; other emitters must use their own module namespace.
		if !strings.HasPrefix(e.Name, m.Name+".") &&
			!(m.Name == KernelName && slices.Contains(KernelEventNames(), e.Name)) {
			v.add("module %q: event %q is not namespaced by the module that emits it", m.Name, e.Name)
		}
	}
	// Require one payload shape per event name, using the events owner’s
	// wire-shape comparison, and collect its diagnostics with other manifest errors.
	for _, problem := range events.OneShapePerName(m.Emits()) {
		v.add("module %q: %s", m.Name, problem)
	}
}

func (v *validation) references(mods []Module) {
	// Nav and subscriptions are checked after every module has been read,
	// because both point at something another module owns: a link is about what
	// the reader may see, and a subscription is about what somebody else emits.
	emitted := map[string]bool{}
	for _, e := range KernelEventNames() {
		emitted[e] = true
	}
	for _, m := range mods {
		for _, e := range m.Emits() {
			emitted[e.Name] = true
		}
	}
	for _, m := range mods {
		v.moduleReferences(m, emitted)
	}
}

func (v *validation) moduleReferences(m Module, emitted map[string]bool) {
	// The handler once, with no name: a second subscription beside it would
	// be one the expansion silently ignored.
	if m.SubscribeAll && len(m.Subscriptions) != 1 {
		v.add("module %q: SubscribeAll declares %d subscriptions; it takes exactly one, the handler every event goes to",
			m.Name, len(m.Subscriptions))
	}
	for _, s := range m.Subscriptions {
		switch {
		case s.Handler == nil:
			v.add("module %q: subscription to %q has no handler", m.Name, s.Name)
		case s.Module != m.Name:
			v.add("module %q: subscription to %q is attributed to module %q", m.Name, s.Name, s.Module)
		case m.SubscribeAll && s.Name == "":
			// Unexpanded, which is a composition nobody ran through
			// Expand. Validate is called by kit/app, which expands first.
		case !emitted[s.Name]:
			v.add("module %q: subscribes to %q, which no module emits", m.Name, s.Name)
		}
	}
	for _, n := range m.Nav {
		if !screenName.MatchString(n.Screen) {
			v.add("module %q: nav entry %q names Screen %q; a nav entry names its screen relative to the workspace — %q", m.Name, n.Label, n.Screen, "task/tasks")
		}
		if n.Permission == "" {
			v.add("module %q: nav entry %q declares no permission; a link everyone sees is still a decision", m.Name, n.Label)
			continue
		}
		if _, ok := v.owner[n.Permission]; !ok {
			v.add("module %q: nav entry %q requires permission %q, which no module defines", m.Name, n.Label, n.Permission)
		}
	}
	// A stream is a door onto an event, so it is checked beside the subscription
	// that is the other door onto the same set: the event has to exist, and the
	// policy has to name someone who may walk through it.
	for _, problem := range streamProblems(m, emitted, v.owner) {
		v.add("%s", problem)
	}
}
