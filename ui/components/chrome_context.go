package components

// chrome_context.go names the workspace in the header, for the widths where the
// sidebar that names it is not shown.

import (
	"strings"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/ui/style"
)

// ChromeContextProps is one line of header chrome that names what a person is
// looking at: the tenant, the workspace, the installation.
type ChromeContextProps struct {
	ComponentProps

	// Name is the workspace's own name, or the host the request arrived at. An
	// empty name draws nothing: a line that says nothing is not chrome, and an
	// empty paragraph is a gap nobody asked for.
	Name string `json:"name"`
	// Size is the body step the rest of the chrome takes, so that naming the
	// workspace does not add a second size to a header the design floor counts.
	Size string `json:"size,omitempty"`
}

// ChromeContext is the workspace's name in the header, hidden from the large
// breakpoint up. It is the mirror of SidebarDisclosure, which discloses the
// sidebar's sections below that breakpoint: the admin sidebar paints the workspace
// name from the large breakpoint up and paints nothing below it, so a phone reader
// had no idea which workspace they were in — and a desktop reader was told twice.
// One name, painted at exactly one width at a time, is what "the workspace is named
// once" means while both clients render the same frame.
//
// It is Text with one rule added, not a second paragraph renderer: the frame's
// chrome has one body step, and this line takes it from the same place every other
// chrome sentence does.
func ChromeContext(p ChromeContextProps) g.Node {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil
	}
	props := p.ComponentProps
	props.Class = strings.TrimSpace(clChromeContext.Compile() + " " + strings.TrimSpace(p.Class))
	return Text(TextProps{ComponentProps: props, Content: name, Size: p.Size, Weight: "semibold"})
}

// clChromeContext is the one rule that makes this line the sidebar's mirror rather
// than its duplicate: from the large breakpoint up, where the sidebar paints, this
// is display:none.
var clChromeContext = style.New().Breakpoint(style.BreakpointLG, func(c style.ClassList) style.ClassList {
	return c.Display(style.DisplayHidden)
})
