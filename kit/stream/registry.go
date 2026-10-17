package stream

import (
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/module"
)

// Topic is one event a live connection may watch, and who may watch it. It is a
// projection of a manifest read at composition: the kernel holds no event name of
// its own, so a topic exists only because some module declared it.
type Topic struct {
	// Event is the topic's name, spelled exactly as the module's Emits spells it.
	Event string
	// Policy is the module's own answer to who may watch this event.
	Policy module.StreamPolicy
	// Operator is the answer of the module that *defines* the policy's permission —
	// module.Permission.Operator — and false for a topic whose policy names no
	// permission at all. It is carried here because a stream must ask the same
	// question the route guard asks, and the guard asks it of the declaration: an
	// operator permission streamed to a customer's tenant is the same escalation
	// whether it arrives as a write, a page or a frame, and the one fact that decides
	// it lives in the manifest and not in the tenant's roles.
	Operator bool
	// Module is the manifest that declared it, for the log line that names where
	// a topic came from.
	Module string
}

// Registry is every stream declaration of one composition, by event name.
//
// It is built from the manifests before anything serves, which is what makes an
// undeclared topic a refusal rather than a filter: nothing here can be asked for
// that no module promised, and no request can widen it. module.Validate is the
// check that a declaration makes sense; this is the lookup the connection asks.
type Registry struct {
	byEvent map[string]Topic
}

// NewRegistry collects the stream declarations of the modules a composition
// composes. Two modules cannot declare one event — module.Validate refuses an
// event outside its emitter's namespace — so the map has no collision to resolve
// and this function has no failure to return.
//
// A stream's policy names a permission and does not say what kind it is, so the
// declaration that defines it is read for that: the permission is the whole
// composition's catalogue, exactly as module.Validate reads it and exactly as
// httpx.Declare publishes it.
func NewRegistry(mods []module.Module) *Registry {
	operator := map[string]bool{}
	for _, m := range mods {
		for _, p := range m.Permissions {
			operator[p.Key] = p.Operator
		}
	}
	r := &Registry{byEvent: map[string]Topic{}}
	for _, m := range mods {
		for _, s := range m.Streams {
			r.byEvent[s.Event] = Topic{
				Event: s.Event, Policy: s.Policy, Module: m.Name,
				Operator: operator[s.Policy.Grant()],
			}
		}
	}
	return r
}

// Topic looks one declaration up. The second answer is false for an event no
// composed module streams, which includes an event some other module emits: a
// topic is a declaration, not a name that happens to exist.
func (r *Registry) Topic(event string) (Topic, bool) {
	t, ok := r.byEvent[event]
	return t, ok
}

// Topics is every declared topic, sorted by name, for the boot log and the
// operator's listing. A connection never reads it: it asks by name.
func (r *Registry) Topics() []Topic {
	out := make([]Topic, 0, len(r.byEvent))
	for _, t := range r.byEvent {
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b Topic) int { return strings.Compare(a.Event, b.Event) })
	return out
}
