// Package web renders pk-ui component contracts to HTML with gomponents,
// styled entirely through tw class lists against the PlatformKit design
// system's role variables.
//
// Every renderer follows the same contract:
//
//   - Props in, gomponents Node out — no hidden state, no template files.
//   - Styling comes only from tw ClassLists declared in classlists.go, so an
//     application derives its stylesheet with tw/emission.For(web.ClassLists()...)
//     instead of scanning source. If a renderer used a class its list does not
//     declare, TestRenderedClassesAreDeclared fails.
//   - Accessibility is by construction: labels are associated, icons are
//     hidden from assistive tech, and states carry their ARIA attributes.
//
// The set implemented here is the working subset the PlatformKit admin and
// module pages compose. Contracts without a renderer yet are listed in
// Unimplemented, and the completeness test keeps that list honest — removing
// an entry without adding the renderer fails.
package components

import (
	"sort"
	"strconv"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// baseAttrs renders the shared ComponentProps onto any element.
func baseAttrs(p ComponentProps, extra ...g.Node) []g.Node {
	var nodes []g.Node
	if p.ID != "" {
		nodes = append(nodes, h.ID(p.ID))
	}
	if p.Hidden {
		nodes = append(nodes, g.Attr("hidden"))
	}
	if p.Disabled {
		nodes = append(nodes, h.Disabled())
	}
	// Deterministic attribute order for stable golden tests.
	keys := make([]string, 0, len(p.Attrs))
	for k := range p.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		nodes = append(nodes, g.Attr(k, p.Attrs[k]))
	}
	return append(nodes, extra...)
}

// attrPairs renders a ComponentProps.Attrs map in deterministic order. Form
// controls apply it directly to the control element (not the field wrapper),
// so contract attributes like minlength, autocomplete, or data-* land where
// browsers and scripts read them.
func attrPairs(attrs map[string]string) []g.Node {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	nodes := make([]g.Node, 0, len(keys))
	for _, k := range keys {
		nodes = append(nodes, g.Attr(k, attrs[k]))
	}
	return nodes
}

// htmxAttrs renders HTMXProps as hx-* attributes; zero values emit nothing,
// so plain server-rendered pages carry no HTMX residue.
func htmxAttrs(p HTMXProps) []g.Node {
	var nodes []g.Node
	add := func(name, v string) {
		if v != "" {
			nodes = append(nodes, g.Attr(name, v))
		}
	}
	add("hx-get", p.Get)
	add("hx-post", p.Post)
	add("hx-put", p.Put)
	add("hx-patch", p.Patch)
	add("hx-delete", p.Delete)
	add("hx-target", p.Target)
	add("hx-swap", p.Swap)
	add("hx-trigger", p.Trigger)
	add("hx-include", p.Include)
	add("hx-confirm", p.Confirm)
	add("hx-ext", p.Ext)
	add("hx-indicator", p.Indicator)
	add("hx-disabled-elt", p.DisabledElt)
	add("hx-vals", p.Vals)
	add("hx-push-url", p.PushURL)
	add("hx-select", p.Select)
	if p.Boost {
		add("hx-boost", "true")
	}
	if p.Disable {
		add("hx-disable", "true")
	}
	return nodes
}

// classes joins a declared base ClassList's compiled form with the caller's
// ComponentProps.Class escape hatch.
func classes(compiled, extra string) g.Node {
	if extra != "" {
		compiled += " " + extra
	}
	return h.Class(compiled)
}

// glyph renders a decorative vector. Adjacent text carries the meaning, so
// the Icon atom hides this form from assistive technology.
func glyph(name string) g.Node {
	if !validIconName(name) {
		return nil
	}
	return Icon(IconProps{Name: name})
}

func validIconName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

func itoa(n int) string { return strconv.Itoa(n) }

// fallbackText is value, or fallback when value is empty. Several renderers
// take a caller's label and have a sensible default for it.
func fallbackText(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
