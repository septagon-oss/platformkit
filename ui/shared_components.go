package ui

import (
	"fmt"
	"maps"
	"slices"

	"github.com/septagon-oss/platformkit/ui/css"
)

// Shared component rules target the owning component, so the same geometry and
// measure apply in an application, an export and the authenticated Gallery.
func sharedComponentRules(s *css.Sheet) {
	v := func(name string) css.Value { return css.VarRef(name, "") }
	s.Select("[data-shared-content] p, [data-shared-content] figcaption, [data-component=hero-full-bleed] p", css.Decl("max-width", css.Literal("37.5em")), css.Decl("overflow-wrap", css.Literal("anywhere")))
	s.Select("[data-component=slot-picker] label, [data-component=option-chips] label", css.Decl("padding", css.Literal("0.5rem 1rem")), css.Decl("border", css.Literal("1px solid var(--pk-color-border-default)")), css.Decl("border-radius", v("pk-radius-button")))
	s.Select("[data-component=slot-picker] label:has(input:checked), [data-component=option-chips] label:has(input:checked)", css.Decl("outline", css.Literal("2px solid var(--pk-color-focus)")))
	s.Select("[data-component=slot-picker] label:has(input:disabled), [data-component=option-chips] label:has(input:disabled)", css.Decl("cursor", css.Literal("not-allowed")))
	s.Select("[data-shared-content] [data-state=refused] p", css.Decl("color", v("pk-color-text-primary")))
	s.Select("[data-shared-content] [data-component=alert]", css.Decl("display", css.Literal("block")), css.Decl("padding", css.Literal("1rem")))
	s.Select("[data-shared-content] [data-alert-icon]", css.Decl("float", css.Literal("right")))
	s.Select("[data-component=pricing-tiers] > [data-component=alert]", css.Decl("padding", css.Literal("1.5rem")))
	s.Select("[data-shared-photo] img", css.Decl("width", css.Literal("100%")), css.Decl("height", css.Literal("auto")), css.Decl("aspect-ratio", css.Literal("auto")))
	s.Select("[data-shared-photo] > [aria-busy]", css.Decl("height", css.Literal("100%")), css.Decl("aspect-ratio", css.Literal("auto")))
	s.Select("[data-photo-content] img", css.Decl("max-height", css.Literal("60vh")), css.Decl("object-fit", css.Literal("contain")))
	s.Select("[data-masonry-item]", css.Decl("break-inside", css.Literal("avoid")))
	for i := 1; i <= 4; i++ {
		decl := css.Decl("columns", css.Literal(fmt.Sprintf("10rem %d", i)))
		s.Select(fmt.Sprintf("[data-masonry-columns='%d']", i), decl)
		s.Media("(min-width: 40rem)", func(s *css.Sheet) { s.Select(fmt.Sprintf("[data-masonry-sm='%d']", i), decl) })
		s.Media("(min-width: 64rem)", func(s *css.Sheet) { s.Select(fmt.Sprintf("[data-masonry-lg='%d']", i), decl) })
	}
	for _, tone := range []string{"success", "warning", "danger", "info"} {
		token := tone
		if tone == "success" {
			token = "ok"
		}
		s.Select("[data-chart-tone="+tone+"], .pk-map-marker[data-tone="+tone+"]", css.Decl("color", v("pk-color-status-"+token)))
	}
	s.Select("[data-component=buy-bar]", css.Decl("position", css.Literal("sticky")), css.Decl("bottom", css.Literal("0")), css.Decl("padding", css.Literal("1rem max(1rem, env(safe-area-inset-right)) max(1rem, env(safe-area-inset-bottom)) max(1rem, env(safe-area-inset-left))")), css.Decl("background", v("pk-color-surface-primary")), css.Decl("z-index", css.Literal("2")))
	s.Select("[data-shared-content] ol, [data-shared-content] ul", css.Decl("padding-inline-start", css.Literal("0")), css.Decl("list-style", css.Literal("none")))
	s.Select("[data-chart-plot]", css.Decl("max-width", css.Literal("100%")), css.Decl("overflow", css.Literal("visible")))
	s.Select("[data-calendar-engine], [data-map-engine]", css.Decl("height", css.Literal("24rem")), css.Decl("width", css.Literal("100%")), css.Decl("max-width", css.Literal("100%")), css.Decl("overflow", css.Literal("auto")), css.Decl("isolation", css.Literal("isolate")))
	s.Select("[data-map-engine]", css.Decl("background", v("pk-color-surface-canvas")))
	s.Select(".pk-map-marker, section [data-map-engine] .leaflet-bar a", css.Decl("display", css.Literal("inline-flex")), css.Decl("align-items", css.Literal("center")), css.Decl("justify-content", css.Literal("center")), css.Decl("min-width", css.Literal("44px")), css.Decl("min-height", css.Literal("44px")), css.Decl("background", css.Literal("transparent")), css.Decl("color", v("pk-color-text-primary")))
	s.Select(".pk-map-pin, [data-map-engine] .leaflet-bar", css.Decl("background", v("pk-color-surface-primary")), css.Decl("border", css.Literal("1px solid var(--pk-color-border-default)")))
	s.Select(".pk-map-marker:focus-visible", css.Decl("outline", css.Literal("2px solid var(--pk-color-focus)")))
	s.Select(".pk-calendar-event", css.Decl("min-height", css.Literal("44px")))
	calendarTokens := map[string]string{
		"background": "surface-primary", "foreground": "text-primary", "border": "border-default", "primary": "accent-default", "highlight": "surface-canvas", "faint": "surface-canvas", "faint-foreground": "text-muted", "strong": "surface-canvas", "muted": "surface-canvas", "muted-foreground": "text-muted", "button": "surface-primary", "button-foreground": "text-primary", "button-border": "border-default", "button-strong": "surface-canvas", "button-strong-border": "border-default", "button-outline": "focus", "ring-color": "focus", "now": "accent-default", "today": "surface-canvas",
	}
	for _, key := range slices.Sorted(maps.Keys(calendarTokens)) {
		token := calendarTokens[key]
		s.Select("[data-calendar-engine]", css.Decl("--fc-classic-"+key, v("pk-color-"+token)))
	}
}
