package style

// resolve.go turns one compiled tw class string into CSS declarations. The
// resolver recognizes exactly the classes tw's typed API can produce from its
// finite enumerations; TestBaseCoversEveryEnumerableClass drives every
// enumerator through the builder and asserts resolution, so the two cannot
// drift. Arbitrary-valued escape hatches (Raw and the *Raw methods)
// are deliberately unresolvable: the caller owns that CSS, and a typo fails
// closed here instead of emitting a wrong rule.

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/ui/css"
)

// ErrUnknownClass reports a class outside the enumerable universe.
var ErrUnknownClass = errors.New("emission: class is not part of tw's enumerable universe")

// ErrUnsupportedState reports a state prefix with no defined CSS mapping yet.
var ErrUnsupportedState = errors.New("emission: state prefix has no CSS mapping")

type decl struct{ prop, value string }

// resolveBase returns the declarations for an unprefixed class, or
// ErrUnknownClass. Marker classes (group, peer) resolve to zero declarations.
func resolveBase(class string) ([]decl, error) {
	if d, ok := fixedClasses[class]; ok {
		return d, nil
	}
	if d, ok := resolveColor(class); ok {
		return d, nil
	}
	if d, ok := resolveViewportHeight(class); ok {
		return d, nil
	}
	if d, ok := resolveSpacingFamily(class); ok {
		return d, nil
	}
	if d, ok := resolveParametric(class); ok {
		return d, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownClass, class)
}

func resolveViewportHeight(class string) ([]decl, bool) {
	for _, value := range AllViewportHeights() {
		if class == "h-["+string(value)+"vh]" {
			return []decl{{"height", string(value) + "vh"}}, true
		}
		if class == "max-h-["+string(value)+"vh]" {
			return []decl{{"max-height", string(value) + "vh"}}, true
		}
	}
	return nil, false
}

// resolveColor handles the four color families, including the /opacity form
// compiled by BgOpacity and friends: bg-<role>/<pct>.
func resolveColor(class string) ([]decl, bool) {
	family, rest, pct := "", "", ""
	for _, f := range []struct{ prefix, prop string }{
		{"bg-", "background-color"},
		{"text-", "color"},
		// Side border colors before the generic prefix: longest match wins.
		{"border-t-", "border-top-color"},
		{"border-b-", "border-bottom-color"},
		{"border-l-", "border-left-color"},
		{"border-r-", "border-right-color"},
		{"border-", "border-color"},
		{"ring-", "--pk-ring-color"},
		{"accent-", "accent-color"},
	} {
		if strings.HasPrefix(class, f.prefix) {
			family, rest = f.prop, strings.TrimPrefix(class, f.prefix)
			break
		}
	}
	if family == "" {
		return nil, false
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest, pct = rest[:i], rest[i+1:]
	}
	role := Color(rest)
	if _, ok := roleValues()[role]; !ok {
		return nil, false // not a color: e.g. text-sm, border-2, ring-inset
	}
	value := roleVar(role)
	if pct != "" {
		if _, err := strconv.Atoi(pct); err != nil {
			return nil, false
		}
		value = "color-mix(in srgb, " + value + " " + pct + "%, transparent)"
	}
	return []decl{{family, value}}, true
}

// spacingFamilies maps class prefixes whose suffix is a Spacing scale step
// to the CSS properties they set. Negative margins and offsets compile with a
// leading '-', handled below.
var spacingFamilies = map[string][]string{
	"p-": {"padding"}, "px-": {"padding-left", "padding-right"},
	"py-": {"padding-top", "padding-bottom"},
	"pl-": {"padding-left"}, "pr-": {"padding-right"},
	"pt-": {"padding-top"}, "pb-": {"padding-bottom"},
	"m-": {"margin"}, "mx-": {"margin-left", "margin-right"},
	"my-": {"margin-top", "margin-bottom"},
	"ml-": {"margin-left"}, "mr-": {"margin-right"},
	"mt-": {"margin-top"}, "mb-": {"margin-bottom"},
	"gap-": {"gap"}, "gap-x-": {"column-gap"}, "gap-y-": {"row-gap"},
	"top-": {"top"}, "bottom-": {"bottom"}, "left-": {"left"}, "right-": {"right"},
	"inset-":   {"inset"},
	"inset-x-": {"left", "right"}, "inset-y-": {"top", "bottom"},
	"w-": {"width"}, "h-": {"height"}, "max-h-": {"max-height"},
	"min-w-": {"min-width"}, "min-h-": {"min-height"},
	"underline-offset-": {"text-underline-offset"},
}

func resolveSpacingFamily(class string) ([]decl, bool) {
	neg := strings.HasPrefix(class, "-")
	c := strings.TrimPrefix(class, "-")

	// Fractional overlay anchors are intentionally separate from the spacing
	// scale so utilities such as padding-1/2 cannot enter the closed universe.
	for _, position := range []struct{ prefix, property string }{
		{"top-", "top"},
		{"right-", "right"},
		{"bottom-", "bottom"},
		{"left-", "left"},
	} {
		if strings.HasPrefix(c, position.prefix) && strings.TrimPrefix(c, position.prefix) == "1/2" {
			value := "50%"
			if neg {
				value = "-50%"
			}
			return []decl{{position.property, value}}, true
		}
	}

	// Longest matching prefix wins (gap-x- before gap-).
	var prefixes []string
	for p := range spacingFamilies {
		prefixes = append(prefixes, p)
	}
	sort.Slice(prefixes, func(i, j int) bool { return len(prefixes[i]) > len(prefixes[j]) })
	for _, p := range prefixes {
		if !strings.HasPrefix(c, p) {
			continue
		}
		v, ok := spacingCSS(strings.TrimPrefix(c, p))
		if !ok {
			continue
		}
		if neg {
			if v == "0px" {
				neg = false
			}
			if neg {
				v = "-" + v
			}
		}
		var out []decl
		for _, prop := range spacingFamilies[p] {
			out = append(out, decl{prop, v})
		}
		return out, true
	}

	// translate-x-/translate-y- carry their own scale (halves + neg- forms
	// via the Translate type, spacing steps via TranslateXStep/YStep).
	for _, t := range []struct{ prefix, prop string }{
		{"translate-x-", "--pk-translate-x"}, {"translate-y-", "--pk-translate-y"},
	} {
		if strings.HasPrefix(c, t.prefix) {
			suffix := strings.TrimPrefix(c, t.prefix)
			if neg {
				suffix = "neg-" + suffix
			}
			if v, ok := translateCSS(suffix); ok {
				return []decl{{t.prop, v}, {"transform", transformValue}}, true
			}
		}
	}

	// divide-x-/divide-y- and space-x-/space-y- apply between children; the
	// declarations here pair with the child-combinator selector emitted by
	// Rules.
	for _, t := range []struct {
		prefix string
		props  []string
	}{
		{"divide-x-", []string{"border-left-width"}},
		{"divide-y-", []string{"border-top-width"}},
		{"space-x-", []string{"margin-left"}},
		{"space-y-", []string{"margin-top"}},
	} {
		if strings.HasPrefix(c, t.prefix) {
			if v, ok := spacingCSS(strings.TrimPrefix(c, t.prefix)); ok {
				if neg {
					v = "-" + v
				}
				var out []decl
				for _, prop := range t.props {
					out = append(out, decl{prop, v})
				}
				return out, true
			}
		}
	}
	return nil, false
}

// resolveParametric handles the enumerations whose suffix selects from a
// finite table, plus the bounded integer families (grid-cols, col-span,
// line-clamp, z-[N]).
func resolveParametric(class string) ([]decl, bool) {
	table := []struct {
		prefix  string
		lookup  map[string]string
		props   []string
		twoDecl func(v string) []decl
	}{
		{prefix: "font-", lookup: fontWeights, props: []string{"font-weight"}},
		{prefix: "font-", lookup: fontFamilies, props: []string{"font-family"}},
		{prefix: "tracking-", lookup: trackings, props: []string{"letter-spacing"}},
		{prefix: "leading-", lookup: leadings, props: []string{"line-height"}},
		{prefix: "max-w-", lookup: maxWidths, props: []string{"max-width"}},
		{prefix: "ease-", lookup: easings, props: []string{"transition-timing-function"}},
	}
	for _, t := range table {
		if strings.HasPrefix(class, t.prefix) {
			if v, ok := t.lookup[strings.TrimPrefix(class, t.prefix)]; ok {
				return []decl{{t.props[0], v}}, true
			}
		}
	}

	// text-<size> vs text-<align> (text-<color> was handled earlier).
	if s := strings.TrimPrefix(class, "text-"); s != class {
		if fs, ok := fontSizes[s]; ok {
			return []decl{{"font-size", fs[0]}, {"line-height", fs[1]}}, true
		}
		switch s {
		case "left", "center", "right", "justify":
			return []decl{{"text-align", s}}, true
		}
	}

	// rounded family, with optional side and the bare "rounded" base.
	if class == "rounded" || strings.HasPrefix(class, "rounded-") {
		if d, ok := resolveRounded(class); ok {
			return d, true
		}
	}

	// shadow family with bare "shadow" base.
	if class == "shadow" {
		return shadowDecls(""), true
	}
	if s := strings.TrimPrefix(class, "shadow-"); s != class {
		if _, ok := shadows[s]; ok {
			return shadowDecls(s), true
		}
	}

	// borders: bare "border", border-<width>, border-<side>[-width],
	// border-<style>. Colors were handled by resolveColor.
	if d, ok := resolveBorder(class); ok {
		return d, true
	}

	// rings.
	if d, ok := resolveRing(class); ok {
		return d, true
	}

	if s := strings.TrimPrefix(class, "opacity-"); s != class {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 100 {
			v := "1"
			if n < 100 {
				v = "0." + fmt.Sprintf("%02d", n)
				v = strings.TrimSuffix(v, "0")
				if n == 0 {
					v = "0"
				}
			}
			return []decl{{"opacity", v}}, true
		}
	}
	if s := strings.TrimPrefix(class, "duration-"); s != class {
		if _, err := strconv.Atoi(s); err == nil {
			return []decl{{"transition-duration", s + "ms"}}, true
		}
	}
	if s := strings.TrimPrefix(class, "list-"); s != class {
		switch s {
		case "none", "disc", "decimal":
			return []decl{{"list-style-type", s}}, true
		}
	}
	if s := strings.TrimPrefix(class, "cursor-"); s != class {
		return []decl{{"cursor", s}}, true
	}
	if s := strings.TrimPrefix(class, "select-"); s != class {
		return []decl{{"user-select", s}}, true
	}
	if s := strings.TrimPrefix(class, "pointer-events-"); s != class {
		return []decl{{"pointer-events", s}}, true
	}
	if s := strings.TrimPrefix(class, "outline-"); s != class {
		switch s {
		case "none":
			return []decl{{"outline", "2px solid transparent"}, {"outline-offset", "2px"}}, true
		case "solid", "dashed", "dotted", "double":
			return []decl{{"outline-style", s}}, true
		}
	}
	if s := strings.TrimPrefix(class, "transition-"); s != class {
		if props, ok := transitionProps[s]; ok {
			if s == "none" {
				return []decl{{"transition-property", "none"}}, true
			}
			return []decl{
				{"transition-property", props},
				{"transition-timing-function", "cubic-bezier(0.4, 0, 0.2, 1)"},
				{"transition-duration", "150ms"},
			}, true
		}
	}
	for _, ov := range []struct{ prefix, prop string }{
		{"overflow-x-", "overflow-x"}, {"overflow-y-", "overflow-y"}, {"overflow-", "overflow"},
	} {
		if s := strings.TrimPrefix(class, ov.prefix); s != class {
			switch s {
			case "auto", "hidden", "visible", "scroll", "clip":
				return []decl{{ov.prop, s}}, true
			}
		}
	}
	if s := strings.TrimPrefix(class, "grid-cols-"); s != class {
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 12 {
			return []decl{{"grid-template-columns", "repeat(" + s + ", minmax(0, 1fr))"}}, true
		}
	}
	if class == "col-span-full" {
		return []decl{{"grid-column", "1 / -1"}}, true
	}
	if s := strings.TrimPrefix(class, "col-span-"); s != class {
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 12 {
			return []decl{{"grid-column", "span " + s + " / span " + s}}, true
		}
	}
	if s := strings.TrimPrefix(class, "line-clamp-"); s != class {
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 10 {
			return []decl{
				{"overflow", "hidden"}, {"display", "-webkit-box"},
				{"-webkit-box-orient", "vertical"}, {"-webkit-line-clamp", s},
			}, true
		}
	}
	zc, zneg := class, false
	if strings.HasPrefix(zc, "-z-[") {
		zc, zneg = zc[1:], true
	}
	if s := strings.TrimPrefix(zc, "z-["); s != zc {
		if v, ok := strings.CutSuffix(s, "]"); ok {
			if _, err := strconv.Atoi(v); err == nil {
				if zneg {
					v = "-" + v
				}
				return []decl{{"z-index", v}}, true
			}
		}
	}
	return nil, false
}

func resolveRounded(class string) ([]decl, bool) {
	props := map[string][]string{
		"":  {"border-radius"},
		"t": {"border-top-left-radius", "border-top-right-radius"},
		"b": {"border-bottom-left-radius", "border-bottom-right-radius"},
		"l": {"border-top-left-radius", "border-bottom-left-radius"},
		"r": {"border-top-right-radius", "border-bottom-right-radius"},
	}
	rest := strings.TrimPrefix(class, "rounded")
	rest = strings.TrimPrefix(rest, "-")
	side, size := "", rest
	if len(rest) >= 1 {
		if _, isSide := props[rest[:1]]; isSide && (len(rest) == 1 || rest[1] == '-') {
			side = rest[:1]
			size = strings.TrimPrefix(rest[1:], "-")
		}
	}
	v, ok := radii[size]
	if !ok {
		return nil, false
	}
	var out []decl
	for _, p := range props[side] {
		out = append(out, decl{p, v})
	}
	return out, true
}

func shadowDecls(size string) []decl {
	return []decl{{"box-shadow", shadows[size]}}
}

func resolveBorder(class string) ([]decl, bool) {
	if class == "border" {
		return []decl{{"border-width", "1px"}}, true
	}
	sides := map[string]string{"t": "border-top-width", "b": "border-bottom-width",
		"l": "border-left-width", "r": "border-right-width"}
	if s := strings.TrimPrefix(class, "border-"); s != class {
		switch s {
		case "solid", "dashed", "dotted", "double", "none", "hidden":
			return []decl{{"border-style", s}}, true
		case "0", "2", "4", "8":
			return []decl{{"border-width", s + "px"}}, true
		}
		if prop, ok := sides[s]; ok { // border-b → 1px
			return []decl{{prop, "1px"}}, true
		}
		if len(s) > 2 && s[1] == '-' {
			if prop, ok := sides[s[:1]]; ok {
				switch s[2:] {
				case "0", "2", "4", "8":
					return []decl{{prop, s[2:] + "px"}}, true
				}
			}
		}
	}
	return nil, false
}

const ringShadow = "0 0 0 var(--pk-ring-offset-width, 0px) var(--pk-ring-offset-color, #fff), " +
	"0 0 0 calc(var(--pk-ring-width, 0px) + var(--pk-ring-offset-width, 0px)) " +
	"var(--pk-ring-color, var(--pk-role-ring-focus))"

func resolveRing(class string) ([]decl, bool) {
	if class == "ring-inset" {
		return []decl{{"--pk-ring-inset", "inset"}}, true
	}
	if s := strings.TrimPrefix(class, "ring-offset-"); s != class {
		switch s {
		case "0", "1", "2", "4", "8":
			return []decl{{"--pk-ring-offset-width", s + "px"}}, true
		}
	}
	if s := strings.TrimPrefix(class, "ring-"); s != class {
		switch s {
		case "0", "1", "2", "4", "8":
			return []decl{
				{"--pk-ring-width", s + "px"},
				{"box-shadow", ringShadow},
			}, true
		}
	}
	return nil, false
}

const transformValue = "translate(var(--pk-translate-x, 0), var(--pk-translate-y, 0)) rotate(var(--pk-rotate, 0))"

// fixedClasses covers every no-argument builder toggle and each enumeration
// whose members map to fixed declarations.
var fixedClasses = map[string][]decl{
	"rotate-180": {{"--pk-rotate", "180deg"}, {"transform", transformValue}},

	// Display (class name == value except the flow shorthands tw uses).
	"block": {{"display", "block"}}, "inline": {{"display", "inline"}},
	"inline-block": {{"display", "inline-block"}}, "flex": {{"display", "flex"}},
	"inline-flex": {{"display", "inline-flex"}}, "grid": {{"display", "grid"}},
	"inline-grid": {{"display", "inline-grid"}}, "hidden": {{"display", "none"}},
	"contents": {{"display", "contents"}}, "table": {{"display", "table"}},
	"inline-table": {{"display", "inline-table"}}, "table-caption": {{"display", "table-caption"}},
	"table-cell": {{"display", "table-cell"}}, "table-column": {{"display", "table-column"}},
	"table-column-group": {{"display", "table-column-group"}},
	"table-footer-group": {{"display", "table-footer-group"}},
	"table-header-group": {{"display", "table-header-group"}},
	"table-row":          {{"display", "table-row"}}, "table-row-group": {{"display", "table-row-group"}},
	"flow-root": {{"display", "flow-root"}}, "list-item": {{"display", "list-item"}},

	// Flex alignment.
	"items-start": {{"align-items", "flex-start"}}, "items-end": {{"align-items", "flex-end"}},
	"items-center": {{"align-items", "center"}}, "items-baseline": {{"align-items", "baseline"}},
	"items-stretch": {{"align-items", "stretch"}},
	"justify-start": {{"justify-content", "flex-start"}}, "justify-end": {{"justify-content", "flex-end"}},
	"justify-center": {{"justify-content", "center"}}, "justify-between": {{"justify-content", "space-between"}},
	"justify-around": {{"justify-content", "space-around"}}, "justify-evenly": {{"justify-content", "space-evenly"}},
	"flex-row": {{"flex-direction", "row"}}, "flex-row-reverse": {{"flex-direction", "row-reverse"}},
	"flex-col": {{"flex-direction", "column"}}, "flex-col-reverse": {{"flex-direction", "column-reverse"}},

	// Position.
	"static": {{"position", "static"}}, "relative": {{"position", "relative"}},
	"absolute": {{"position", "absolute"}}, "fixed": {{"position", "fixed"}},
	"sticky": {{"position", "sticky"}},

	// Flex shorthands.
	"flex-1": {{"flex", "1 1 0%"}}, "flex-none": {{"flex", "none"}},
	"flex-grow": {{"flex-grow", "1"}}, "flex-grow-0": {{"flex-grow", "0"}},
	"flex-shrink-0": {{"flex-shrink", "0"}},
	"flex-wrap":     {{"flex-wrap", "wrap"}}, "flex-nowrap": {{"flex-wrap", "nowrap"}},

	// Typography toggles.
	"italic": {{"font-style", "italic"}}, "not-italic": {{"font-style", "normal"}},
	"underline":       {{"text-decoration-line", "underline"}},
	"no-underline":    {{"text-decoration-line", "none"}},
	"line-through":    {{"text-decoration-line", "line-through"}},
	"no-line-through": {{"text-decoration-line", "none"}},
	"uppercase":       {{"text-transform", "uppercase"}}, "lowercase": {{"text-transform", "lowercase"}},
	"capitalize": {{"text-transform", "capitalize"}}, "normal-case": {{"text-transform", "none"}},
	"tabular-nums": {{"font-variant-numeric", "tabular-nums"}},
	"truncate": {
		{"overflow", "hidden"}, {"text-overflow", "ellipsis"}, {"white-space", "nowrap"},
	},
	"whitespace-nowrap":   {{"white-space", "nowrap"}},
	"whitespace-pre-wrap": {{"white-space", "pre-wrap"}},
	"break-all":           {{"word-break", "break-all"}}, "break-words": {{"overflow-wrap", "break-word"}},

	// Visual toggles.
	"sr-only": {
		{"position", "absolute"}, {"width", "1px"}, {"height", "1px"},
		{"padding", "0"}, {"margin", "-1px"}, {"overflow", "hidden"},
		{"clip", "rect(0, 0, 0, 0)"}, {"white-space", "nowrap"}, {"border-width", "0"},
	},
	"appearance-none": {{"appearance", "none"}},
	"aspect-square":   {{"aspect-ratio", "1 / 1"}}, "aspect-video": {{"aspect-ratio", "16 / 9"}},
	"object-contain": {{"object-fit", "contain"}}, "object-cover": {{"object-fit", "cover"}},
	"overscroll-contain": {{"overscroll-behavior", "contain"}},
	"resize-none":        {{"resize", "none"}}, "resize-y": {{"resize", "vertical"}},
	"transform": {{"transform", transformValue}},

	// Animations reference the keyframes Base() emits.
	"animate-spin":  {{"animation", "pk-spin 1s linear infinite"}},
	"animate-pulse": {{"animation", "pk-pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite"}},

	// Markers carry no declarations of their own.
	"group": {}, "peer": {},
}

// childCombinator is the selector suffix for the between-children families.
const childCombinator = " > :not([hidden]) ~ :not([hidden])"

// needsChildCombinator reports classes whose declarations apply between the
// element's children rather than to the element itself.
func needsChildCombinator(base string) bool {
	for _, p := range []string{"divide-x-", "divide-y-", "space-x-", "space-y-"} {
		if strings.HasPrefix(strings.TrimPrefix(base, "-"), p) {
			return true
		}
	}
	return false
}

// Rules emits the CSS rules for the given compiled class strings, prefixed
// forms included ("md:hover:bg-surface-brand"). Unknown classes and
// unsupported state prefixes fail closed with an error naming the class.
func Rules(classes ...string) (*css.Sheet, error) {
	s := css.NewSheet()
	for _, class := range classes {
		if class == "" {
			continue
		}
		if err := addClassRule(s, class); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func addClassRule(s *css.Sheet, class string) error {
	// Split prefixes: any number of breakpoint: and state: segments before
	// the base class, in tw's compiled order (breakpoint first, then states).
	var media string
	states := []string{}
	base := class
	for {
		i := strings.IndexByte(base, ':')
		if i < 0 {
			break
		}
		prefix := base[:i]
		if bp, ok := breakpoints[prefix]; ok {
			media = "(min-width: " + bp + ")"
			base = base[i+1:]
			continue
		}
		if _, ok := stateSelector(prefix, "x"); ok || prefix == "peer" {
			states = append(states, prefix)
			base = base[i+1:]
			continue
		}
		break
	}

	decls, err := resolveBase(base)
	if err != nil {
		return err
	}
	if len(decls) == 0 { // marker classes: nothing to emit
		return nil
	}

	selector := "." + escapeClass(class)
	for _, st := range states {
		sel, ok := stateSelector(st, escapeClass(class))
		if !ok {
			return fmt.Errorf("%w: %q in %q", ErrUnsupportedState, st, class)
		}
		selector = sel
	}
	if needsChildCombinator(base) {
		selector += childCombinator
	}

	sdecls := make([]css.Declaration, 0, len(decls))
	for _, d := range decls {
		sdecls = append(sdecls, css.Decl(d.prop, css.Literal(d.value)))
	}
	rule := css.Rule{Selector: selector, Decls: sdecls}

	if media != "" {
		s.Media(media, func(inner *css.Sheet) { inner.AddRule(rule) })
		return nil
	}
	s.AddRule(rule)
	return nil
}
