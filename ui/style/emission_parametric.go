package style

import (
	"fmt"
	"strconv"
	"strings"
)

func resolveParametricTable(class string) ([]decl, bool) {
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
	}
	for _, t := range table {
		if strings.HasPrefix(class, t.prefix) {
			if v, ok := t.lookup[strings.TrimPrefix(class, t.prefix)]; ok {
				return []decl{{t.props[0], v}}, true
			}
		}
	}
	if key, ok := strings.CutPrefix(class, "ease-"); ok {
		if value, exists := easings[key]; exists {
			return []decl{{"transition-timing-function", value.css()}}, true
		}
	}
	return nil, false
}

func resolveText(class string) ([]decl, bool) {
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
	return nil, false
}

func resolveEdges(class string) ([]decl, bool) {
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
	return nil, false
}

func resolveOpacityDuration(class string) ([]decl, bool) {
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
		if value, ok := durationCSS(s); ok {
			return []decl{{"transition-duration", value}}, true
		}
	}
	return nil, false
}

func resolveInteraction(class string) ([]decl, bool) {
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
	return nil, false
}

func resolveTransition(class string) ([]decl, bool) {
	if s := strings.TrimPrefix(class, "transition-"); s != class {
		if props, ok := transitionProps[s]; ok {
			if s == "none" {
				return []decl{{"transition-property", "none"}}, true
			}
			duration, _ := durationCSS(string(transitionDuration))
			return []decl{
				{"transition-property", props},
				{"transition-timing-function", easings[string(transitionEasing)].css()},
				{"transition-duration", duration},
			}, true
		}
	}
	return nil, false
}

func resolveOverflow(class string) ([]decl, bool) {
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
	return nil, false
}

func resolveGrid(class string) ([]decl, bool) {
	for _, axis := range []struct{ prefix, property string }{{"grid-cols-", "grid-template-columns"}, {"grid-rows-", "grid-template-rows"}} {
		if s, ok := strings.CutPrefix(class, axis.prefix); ok {
			if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 12 {
				return []decl{{axis.property, "repeat(" + s + ", minmax(0, 1fr))"}}, true
			}
		}
	}
	if class == "col-span-full" {
		return []decl{{"grid-column", "1 / -1"}}, true
	}
	if class == "row-span-full" {
		return []decl{{"grid-row", "1 / -1"}}, true
	}
	if s := strings.TrimPrefix(class, "col-span-"); s != class {
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 12 {
			return []decl{{"grid-column", "span " + s + " / span " + s}}, true
		}
	}
	return nil, false
}

func resolveLineClamp(class string) ([]decl, bool) {
	if s := strings.TrimPrefix(class, "line-clamp-"); s != class {
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 10 {
			return []decl{
				{"overflow", "hidden"}, {"display", "-webkit-box"},
				{"-webkit-box-orient", "vertical"}, {"-webkit-line-clamp", s},
			}, true
		}
	}
	return nil, false
}

func resolveZIndex(class string) ([]decl, bool) {
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
