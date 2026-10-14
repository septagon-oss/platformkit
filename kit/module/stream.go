package module

import (
	"fmt"
	"regexp"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// Stream declares that one of this module's own events may be watched by a live
// connection, and who may watch it.
//
// A stream is a notice and never a payload: a connection is told that an event
// happened and when, and its next read is the truth. An event nothing streams stays
// exactly as it was — the durable path does not consult this list, so declaring a
// stream opens a door for live clients and changes nothing about the handlers, the
// claims or the replay of the event itself.
//
// Which of a module's own events are worth watching, and under which policy, is the
// module's decision and not the kernel's: this holds the declaration, and Validate
// refuses one that names an event the module does not emit.
type Stream struct {
	// Event is an event this module emits, spelled the way Emits spells it.
	Event string
	// Policy names who may watch it. The zero StreamPolicy is refused.
	Policy StreamPolicy
}

// StreamPolicy is who may watch a streamed event. Its fields are unexported and
// its three constructors are the only way to build a usable value, so "some policy
// I did not think of" is not expressible — the same shape httpx.Auth takes for the
// same reason.
//
// The zero StreamPolicy is refused: a stream that says nothing about who may watch
// it is a stream whose author meant one of these three and wrote none.
type StreamPolicy struct {
	kind, grant, field string
}

// The three policies, as values rather than an enum a caller compares: a module
// builds a policy with a constructor and the endpoint asks it, so no reader has to
// know the spellings to use one correctly.
const (
	// KindPrincipal is StreamAnyPrincipal's answer to Kind: any signed-in person of
	// the tenant, for events that are tenant-public metadata — a price recorded, a
	// status moved.
	KindPrincipal = "principal"
	// KindGrant is StreamPermission's answer to Kind: the holder of one permission.
	KindGrant = "grant"
	// KindRecipient is StreamRecipient's answer to Kind: the one person the event's
	// own payload names.
	KindRecipient = "recipient"
)

// StreamAnyPrincipal lets every signed-in person of the tenant watch the event.
// It spends none of the caller's authority, which is why it names no grant: the
// person who may watch is the person who may stand in the workspace.
func StreamAnyPrincipal() StreamPolicy {
	return StreamPolicy{kind: KindPrincipal}
}

// StreamPermission lets the holder of grant watch the event, asked of the
// composition's own authorizer in the connection's tenant — the same question the
// route guard asks, and not a second policy engine beside it.
//
// The grant is re-asked at every heartbeat: a connection that outlives its own
// request must not outlive the grant it was admitted on.
func StreamPermission(grant string) StreamPolicy {
	return StreamPolicy{kind: KindGrant, grant: grant}
}

// StreamRecipient delivers the notice to one person: the server reads the named
// member of the event's payload as a user id, compares it to the principal of each
// connection, and lets no part of the payload leave the server. The field is not
// checked against the declared payload type at Validate — that schema lives in
// kit/events as a Go type, and a second copy of it here would be a second schema to
// keep honest — so the check runs at delivery: a member that is not a user id is a
// notice that reaches nobody, said once in the log with the event, its id and the
// field.
func StreamRecipient(field string) StreamPolicy {
	return StreamPolicy{kind: KindRecipient, field: field}
}

// Kind is which policy this is: StreamPrincipal, StreamGrant or StreamRecipient.
// The empty string is the zero value, which nothing accepts.
func (p StreamPolicy) Kind() string { return p.kind }

// Grant is the permission a StreamGrant policy names, and empty for the others.
func (p StreamPolicy) Grant() string { return p.grant }

// Field is the payload member a StreamRecipient policy reads, and empty for the
// others.
func (p StreamPolicy) Field() string { return p.field }

// jsonMember is the grammar of a payload member name as this kernel spells its
// payloads: an identifier, lower-case first, camelCase within. A stream that names
// anything else is a stream that will never match a payload.
var jsonMember = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*$`)

// streamProblems is one manifest's stream declarations, refused. The emitted set
// is the one Validate already computed for subscriptions, because the rule is the
// same rule seen from the other side: a stream is a door onto an event, and an event
// nobody emits has nothing behind the door.
func streamProblems(m Module, emitted map[string]bool, owner map[string]string) []string {
	var bad []string
	add := func(format string, args ...any) { bad = append(bad, fmt.Sprintf(format, args...)) }
	seen := map[string]bool{}
	for _, s := range m.Streams {
		if !emitted[s.Event] {
			add("module %q: streams %q, which it does not emit", m.Name, s.Event)
			continue
		}
		if seen[s.Event] {
			add("module %q: stream %q is declared twice", m.Name, s.Event)
			continue
		}
		seen[s.Event] = true
		switch s.Policy.Kind() {
		case "":
			add("module %q: stream %q declares no policy; every stream names who may watch it", m.Name, s.Event)
		case KindPrincipal:
		case KindGrant:
			if !httpx.ValidPermission(s.Policy.Grant()) {
				add("module %q: stream %q requires %q; a permission is %q, both lower-case identifiers",
					m.Name, s.Event, s.Policy.Grant(), "<resource>:<action>")
				continue
			}
			if _, ok := owner[s.Policy.Grant()]; !ok {
				add("module %q: stream %q requires permission %q, which no module defines", m.Name, s.Event, s.Policy.Grant())
			}
		case KindRecipient:
			if !jsonMember.MatchString(s.Policy.Field()) {
				add("module %q: stream %q names recipient field %q; a payload member is an identifier that starts lower-case",
					m.Name, s.Event, s.Policy.Field())
			}
		default:
			add("module %q: stream %q declares policy %q, which is not one of the three constructors",
				m.Name, s.Event, s.Policy.Kind())
		}
	}
	return bad
}
