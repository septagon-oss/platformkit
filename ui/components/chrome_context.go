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

// ChromeContext names the workspace below the sidebar's large breakpoint.
// Empty or whitespace-only names render nothing; Size follows Text's defaults.
func ChromeContext(p ChromeContextProps) g.Node {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil
	}
	props := p.ComponentProps
	props.Class = strings.TrimSpace(clChromeContext.Compile() + " " + strings.TrimSpace(p.Class))
	return Text(TextProps{ComponentProps: props, Element: "span", Content: name, Size: p.Size, Weight: "semibold"})
}

// The base display makes the breakpoint override explicit to the class compiler.
var clChromeContext = style.New().Display(style.DisplayInline).
	Breakpoint(style.BreakpointLG, func(c style.ClassList) style.ClassList {
		return c.Display(style.DisplayHidden)
	})
