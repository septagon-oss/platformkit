package components

import (
	"cmp"
	"slices"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// VideoProps describes native browser playback. There is deliberately no
// autoplay: loading a document must not start sound or motion. The composing
// page owns access-protected URLs, a nearby transcript, errors and saved position.
type VideoProps struct {
	ComponentProps
	Label   string        `json:"label"`
	Sources []VideoSource `json:"sources"`
	Tracks  []VideoTrack  `json:"tracks,omitempty"`
	Poster  string        `json:"poster,omitempty"`
}

type VideoSource struct {
	Src  string `json:"src"`
	Type string `json:"type"`
}

type VideoTrack struct {
	Src      string `json:"src"`
	Language string `json:"language"`
	Label    string `json:"label"`
	Kind     string `json:"kind,omitempty"` // captions (default), subtitles, descriptions, chapters
	Default  bool   `json:"default,omitzero"`
}

// Video keeps seeking, volume, captions and fullscreen in the user agent's
// keyboard-accessible controls. Disabled players contain no media sources.
func Video(p VideoProps) g.Node {
	base := p.ComponentProps
	base.Disabled = false // video has no native disabled attribute
	nodes := baseAttrs(base, classes(clVideo.Compile(), p.Class), g.Attr("aria-label", p.Label), g.Attr("playsinline"), g.Attr("preload", "metadata"))
	if p.Poster != "" {
		nodes = append(nodes, g.Attr("poster", p.Poster))
	}
	if p.Disabled {
		return h.Video(append(nodes, g.Attr("aria-disabled", "true"))...)
	}
	nodes = append(nodes, g.Attr("controls"))
	for _, source := range p.Sources {
		nodes = append(nodes, h.Source(h.Src(source.Src), h.Type(source.Type)))
	}
	defaultUsed := false
	for _, track := range p.Tracks {
		kind := cmp.Or(track.Kind, "captions")
		if !slices.Contains([]string{"captions", "subtitles", "descriptions", "chapters"}, kind) {
			kind = "captions"
		}
		selected := track.Default && !defaultUsed
		defaultUsed = defaultUsed || selected
		nodes = append(nodes, g.El("track", h.Src(track.Src), g.Attr("kind", kind), g.Attr("srclang", track.Language), g.Attr("label", track.Label), g.If(selected, g.Attr("default"))))
	}
	return h.Video(nodes...)
}
