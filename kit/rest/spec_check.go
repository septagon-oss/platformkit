package rest

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// check refuses a Spec that could only produce broken routes or unroutable
// events. It panics, at the mount site, for the same reason httpx.Permission
// does: this is a wiring mistake, not a request-time condition.
func (s Spec[T]) check() {
	if s.Create != nil && s.History {
		panic("rest: Create replaces the create steps, so a History resource would never record its first version")
	}
	s.checkSource()
	bad := s.choiceFault()
	if bad == "" {
		bad = s.identityFault()
	}
	if bad == "" {
		bad = s.operationsFault()
	}
	if bad == "" {
		bad = s.historyFault()
	}
	if bad == "" {
		bad = s.gateFault()
	}
	if bad == "" {
		bad = widgetFault(s.choiceFields())
	}
	if bad == "" {
		bad = s.translationFault()
	}
	if bad == "" {
		bad = s.richTextFault()
	}
	if bad == "" {
		bad = presentationFault(s.choiceFields())
	}
	if bad == "" {
		bad = s.pictureFault()
	}
	if bad == "" {
		bad = s.entryHintFault()
	}
	if bad == "" {
		bad = fieldHintFault(s.Entity, s.choiceFields(), s.Present)
	}
	if bad == "" {
		bad = displayFieldFault(s.choiceFields())
	}
	if bad == "" {
		bad = conditionFault(s.choiceFields())
	}
	if bad == "" {
		bad = formHintsFault(httpx.Resource{Schema: s.Schema(), Present: s.Present, Immutable: s.Immutable, Operations: s.Operations})
	}
	if bad != "" {
		panic("rest: Spec for " + s.Module + "." + s.Entity + ": " + bad)
	}
}

// operationsFault names what this Spec's operation set and its two declared
// guards get wrong, and "" when they are coherent. Every refusal here is a
// mount-time panic, like the rest of check: the alternative to refusing the
// mount is a route table that answers a verb nobody declared, a page whose
// button leads to a 404, or a guard that guards nothing.
//
// The set is checked before the guards because a guard is only coherent beside
// the routes it guards: the refusal of a signed-in write is a sentence about
// Update and Delete being offered.
func (s Spec[T]) operationsFault() string {
	seen := map[httpx.CRUD]bool{}
	for _, c := range s.Operations {
		if !httpx.ValidCRUD(string(c)) {
			return fmt.Sprintf("Operations names %q, which is not one of list, read, create, update or delete", string(c))
		}
		if seen[c] {
			return fmt.Sprintf("Operations names %q twice; an operation set is a set, and the catalog would publish the verb twice", string(c))
		}
		seen[c] = true
	}
	// An empty Operations means all five, and nil and an empty slice both mean
	// it: nothing here refuses it, and nothing here reads it as "none".
	//
	// A list with no read is legal: the collection page is mounted and its rows
	// are drawn unlinked, because ui/resource asks whether a record route exists
	// before it links a row to one. What stays refused is a door drawn in front of
	// no route, which is what the operation set decides.
	for _, d := range []struct {
		field, shorthand string
		auth             httpx.Auth
		operator         bool
	}{{"ReadAuth", s.Read, s.ReadAuth, s.OperatorRead}, {"WriteAuth", s.Write, s.WriteAuth, s.OperatorWrite}} {
		if !d.auth.Declared() {
			continue
		}
		switch {
		case d.shorthand != "":
			return fmt.Sprintf("declares %s and the permission %q; a guard is stated once, and %s overrides the permission it would be spelled with",
				d.field, d.shorthand, d.field)
		case d.operator:
			return fmt.Sprintf("declares %s with Operator%s; the declaration says whether it is the operator's — httpx.OperatorPermission carries that fact itself",
				d.field, strings.TrimSuffix(d.field, "Auth"))
		case d.auth.IsPublic():
			// Public would be a tenant's rows behind no guard at all. A public
			// face has a door of its own — rest.Singleton's Public and Face — and a
			// route a module mounts itself has both the surface and the sentence.
			return fmt.Sprintf("%s is public; a Spec serves a tenant's rows, so a public face is rest.Singleton's Face or a route you mount yourself", d.field)
		}
	}
	// A write whose row the caller names, under a guard that decides nothing but
	// membership: the generic PATCH and DELETE reach the row through
	// crud.RecheckTenant, which asks whose tenant and never whose row. The door
	// for this is rest.Command, which carries an Auth and runs the module's own
	// service, where tenancy.RequirePolicy decides the object. The question is
	// asked of the answer rather than of the field, because either spelling of a
	// guard can be the one that names nothing.
	if auth := s.writeAuth(); !auth.NamesAGrant() && (s.offers(httpx.CRUDUpdate) || s.offers(httpx.CRUDDelete)) {
		return "mounts update or delete under a write guard that is no grant; a write about a row the caller names is a rest.Command or a permission, because the generic routes check the tenant and not the row"
	}
	// Immutable is refused at the create and the patch, and shown read-only in
	// the two forms. A guard that guards no door is a misspelled name one level
	// up: it looks like a rule and changes nothing.
	if len(s.Immutable) > 0 && !s.offers(httpx.CRUDCreate) && !s.offers(httpx.CRUDUpdate) {
		return fmt.Sprintf("names %d Immutable field(s) and mounts neither create nor update, so nothing writes them and nothing refuses them", len(s.Immutable))
	}
	return ""
}

// widgetFault returns the first widget outside the presentation vocabulary,
// or empty. Checking at mount avoids silently rendering an unintended control.
func widgetFault(fields []crud.Field) string {
	for _, f := range fields {
		if !entity.ValidWidget(f.Widget) {
			return fmt.Sprintf("field %q names widget %q, which no screen can draw", f.Name, f.Widget)
		}
	}
	return ""
}

func (s Spec[T]) checkSource() {
	if s.Source != nil {
		if s.Source.Get == nil || s.Source.List == nil {
			panic("rest: Source requires Get and List")
		}
		if s.offers(httpx.CRUDCreate) || s.offers(httpx.CRUDUpdate) || s.offers(httpx.CRUDDelete) || s.Present.Highlight != nil || s.Present.TrendField != "" {
			panic("rest: Source requires read-only operations without stored highlights or trends")
		}
	}
}

func (s Spec[T]) identityFault() string {
	var bad string
	switch {
	case !strings.HasPrefix(s.Path, "/"):
		bad = fmt.Sprintf("Path %q does not start with /", s.Path)
	case s.Path == "/"+s.Module || strings.HasPrefix(s.Path, "/"+s.Module+"/") ||
		strings.HasPrefix(s.Path, "/api/v1") || strings.HasPrefix(s.Path, "/app") ||
		strings.HasPrefix(s.Path, "/ops") || strings.HasPrefix(s.Path, "/public"):
		bad = fmt.Sprintf("Path %q must be relative to the module — %q", s.Path, "/"+strings.TrimPrefix(strings.TrimPrefix(s.Path, "/api/v1/"+s.Module), "/"))
	case s.Module == "":
		// Checked before the event name below, which needs the module to exist:
		// the fault is the missing name itself, and Mount fills it from the router
		// a Spec mounts on, so an empty one here is a Spec nobody mounted.
		bad = fmt.Sprintf("Module %q names no module; Mount fills it from the router the Spec mounts on, and a Spec checked before that names the module its events belong to", s.Module)
	case !events.ValidName(s.Event(Created)):
		bad = fmt.Sprintf("Module %q and Entity %q do not make an event name", s.Module, s.Entity)
	case !s.ReadAuth.Declared() && !httpx.ValidPermission(s.Read):
		// A declared guard replaces the shorthand rather than adding to it, so
		// an empty Read beside ReadAuth is the shape R3 insists on and not the
		// typo this case refuses. operationsFault names the pair incoherent when
		// both are spelled.
		bad = fmt.Sprintf("Read %q is not %q", s.Read, "<resource>:<action>")
	case !s.WriteAuth.Declared() && !httpx.ValidPermission(s.Write):
		bad = fmt.Sprintf("Write %q is not %q", s.Write, "<resource>:<action>")
	}
	// An Immutable name the entity has no field for guards nothing, silently
	// and forever, so it is a mount-time panic like everything else here.
	for _, name := range s.Immutable {
		if bad != "" {
			break
		}
		if _, ok := crud.FieldNamed(s.choiceFields(), name); !ok {
			bad = fmt.Sprintf("Immutable names %q, which is not a field of the entity", name)
		}
	}
	return bad
}

func (s *Spec[T]) richTextFault() string {
	var bad string
	for _, field := range s.choiceFields() {
		if field.Widget != "richtext" {
			continue
		}
		if field.Type != entity.TypeString && field.Type != entity.TypeText {
			bad = fmt.Sprintf("richtext field %q must be a string", field.Name)
			break
		}
		if s.RichTextFiles == nil {
			bad = fmt.Sprintf("richtext field %q needs a Files port", field.Name)
			break
		}
		// A missing FileUses keeps the no-op recorder and warns once for this Spec.
		// Unlike a missing Files resolver, it does not prevent writing richtext.
		if s.FileUses == nil {
			slog.Warn("rest: "+s.Module+"."+s.Entity+" has a richtext field and no FileUses port; "+
				"it will record no use, so nothing can ask which record shows its files",
				"module", s.Module, "entity", s.Entity)
			s.FileUses = RecordNoUses{}
		}
	}
	return bad
}

func (s Spec[T]) pictureFault() string {
	var bad string
	for _, f := range s.choiceFields() {
		if f.Type == entity.TypePicture && (s.offers(httpx.CRUDCreate) || s.offers(httpx.CRUDUpdate)) && !slices.Contains(s.Immutable, f.Name) {
			bad = fmt.Sprintf("picture field %q must be Immutable", f.Name)
			break
		}
	}
	return bad
}
