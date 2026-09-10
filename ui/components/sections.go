package components

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// SectionHeaderProps keeps document structure independent of presentation.
type SectionHeaderProps struct {
	ComponentProps
	Eyebrow     string `json:"eyebrow,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Level       int    `json:"level,omitzero" enum:"0,1,2,3,4,5,6"`
	Size        int    `json:"size,omitzero" enum:"0,1,2,3,4,5,6"`
}

// SectionHeader is shared by page introductions and sections within a page.
func SectionHeader(p SectionHeaderProps) g.Node {
	var content []g.Node
	if p.Eyebrow != "" {
		content = append(content, Text(TextProps{Content: p.Eyebrow, Size: "sm", Color: "muted"}))
	}
	content = append(content, Heading(HeadingProps{Text: p.Title, Level: p.Level, Size: p.Size}))
	if p.Description != "" {
		content = append(content, Text(TextProps{Content: p.Description, Color: "muted"}))
	}
	return h.Header(append(baseAttrs(p.ComponentProps), Stack(StackProps{Gap: "3"}, content...))...)
}

// SectionProps supplies a bounded content region without imposing a card.
type SectionProps struct {
	ComponentProps
	MaxWidth string `json:"maxWidth,omitempty" enum:",sm,md,lg,xl,2xl,4xl,7xl,full"`
	Gap      string `json:"gap,omitempty"`
}

type SectionSlots struct {
	Header []g.Node
	Body   []g.Node
	Footer []g.Node
}

// Section composes trusted regions through the existing Container and Stack.
func Section(p SectionProps, slots SectionSlots) g.Node {
	return h.Section(append(baseAttrs(p.ComponentProps), Container(ContainerProps{MaxWidth: p.MaxWidth},
		Stack(StackProps{Gap: fallbackText(p.Gap, "6")}, g.Group(slots.Header), g.Group(slots.Body), g.Group(slots.Footer))))...)
}

type HeroProps struct {
	SectionHeaderProps
	MaxWidth string `json:"maxWidth,omitempty" enum:",sm,md,lg,xl,2xl,4xl,7xl,full"`
}

type HeroSlots struct {
	Actions []g.Node
	Media   []g.Node
}

// Hero stacks on small screens and places media beside the introduction from
// the medium breakpoint. Its title defaults to h1; a nested hero can set Level.
func Hero(p HeroProps, slots HeroSlots) g.Node {
	heading := p.SectionHeaderProps
	heading.ComponentProps = ComponentProps{}
	if heading.Level == 0 {
		heading.Level = 1
	}
	copy := Stack(StackProps{Gap: "6"}, SectionHeader(heading), Flex(FlexProps{Wrap: true, Gap: "3"}, slots.Actions...))
	body := g.Node(copy)
	if len(slots.Media) > 0 {
		body = Grid(GridProps{Columns: "1", MD: "2", Gap: "8"}, copy, g.Group(slots.Media))
	}
	return Section(SectionProps{ComponentProps: p.ComponentProps, MaxWidth: p.MaxWidth}, SectionSlots{Body: []g.Node{body}})
}
