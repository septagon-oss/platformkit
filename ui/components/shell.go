package components

// shell.go is the application frame: the parts of an admin page that are not a
// component in a screen but the page around them — the skip link, the sidebar
// column beside a scrolling main, the top bar, the footer, and the one dialog
// every destructive action is confirmed in.
//
// It is here rather than in modules/admin for one reason, and it is the reason
// this package exists at all: a class that no list in classlists.go declares
// gets no rule in the stylesheet, so markup written outside this package would
// render unstyled. The shell has a size and a colour, so the shell is a
// component.

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// ShellProps is the application frame.
type ShellProps struct {
	ComponentProps
	// SkipTarget is the id the skip link jumps to; the main region takes it.
	//
	// There is no SkipLabel beside it. One existed and nothing ever set it: a
	// skip link says "Skip to content" in every application that has one, and a
	// prop nobody writes is a prop every reader of this struct has to rule out.
	SkipTarget string
}

// ShellSlots are the frame's four regions.
type ShellSlots struct {
	Sidebar []g.Node
	Header  []g.Node
	Main    []g.Node
	Footer  []g.Node
}

// Shell renders the frame: a skip link, then a row of the sidebar and a column
// of header, main and footer. The sidebar hides itself below the large
// breakpoint, so a narrow window is one column and no script decided that.
func Shell(p ShellProps, slots ShellSlots) g.Node {
	target := p.SkipTarget
	if target == "" {
		target = "content"
	}
	nodes := baseAttrs(p.ComponentProps)
	nodes = append(nodes, classes(clShell.Compile(), p.Class))
	nodes = append(nodes,
		SkipLink(target, "Skip to content"),
		g.Group(slots.Sidebar),
		h.Div(h.Class(clShellColumn.Compile()),
			h.Header(h.Class(clShellHeader.Compile()), g.Group(slots.Header)),
			h.Main(h.ID(target), h.Class(clShellMain.Compile()), g.Group(slots.Main)),
			h.Footer(h.Class(clShellFooter.Compile()), g.Group(slots.Footer)),
		),
	)
	return h.Div(nodes...)
}

// SkipLink is the first focusable thing on the page: invisible until it is
// focused, and then the fastest route past the navigation. It is a component
// rather than a class string so that its focus treatment is in the stylesheet.
func SkipLink(target, label string) g.Node {
	return h.A(h.Href("#"+target), h.Class(clSkipLink.Compile()), g.Text(label))
}

// ToolbarProps is the row above a table: a title on the left, actions on the
// right, and whatever a screen puts between them.
type ToolbarProps struct {
	ComponentProps
	Title    string
	Subtitle string
}

// Toolbar renders the row.
func Toolbar(p ToolbarProps, actions ...g.Node) g.Node {
	nodes := baseAttrs(p.ComponentProps)
	nodes = append(nodes, classes(clToolbar.Compile(), p.Class))
	var copy []g.Node
	if p.Title != "" {
		copy = append(copy, Heading(HeadingProps{Text: p.Title, Level: 1}))
	}
	if p.Subtitle != "" {
		copy = append(copy, Text(TextProps{Content: p.Subtitle, Size: "sm", Color: "muted"}))
	}
	nodes = append(nodes,
		h.Div(h.Class(clToolbarCopy.Compile()), g.Group(copy)),
		h.Div(h.Class(clToolbarActions.Compile()), g.Group(actions)),
	)
	return h.Div(nodes...)
}

// FormProps is a screen's form: where it posts and what it is called.
type FormProps struct {
	ComponentProps
	HTMXProps
	Action string
	// Label names the form for assistive technology, since a generated form
	// has no visible heading of its own.
	Label string
}

// Form renders a POST form as a stack of fields. The method is always post:
// every write in this application is one, and a form that could GET would be a
// form that put a password in a URL.
func Form(p FormProps, children ...g.Node) g.Node {
	nodes := baseAttrs(p.ComponentProps, htmxAttrs(p.HTMXProps)...)
	nodes = append(nodes,
		classes(clForm.Compile(), p.Class),
		h.Method("post"),
		h.Action(p.Action),
	)
	if p.Label != "" {
		nodes = append(nodes, g.Attr("aria-label", p.Label))
	}
	return h.FormEl(append(nodes, children...)...)
}

// FormActionsProps is the row of buttons at the foot of a form.
type FormActionsProps struct{ ComponentProps }

// FormActions renders the row.
func FormActions(p FormActionsProps, children ...g.Node) g.Node {
	nodes := baseAttrs(p.ComponentProps)
	nodes = append(nodes, classes(clFormActions.Compile(), p.Class))
	return h.Div(append(nodes, children...)...)
}

// ConfirmDialogProps is the one dialog on the page that every destructive
// action is confirmed in.
type ConfirmDialogProps struct {
	ComponentProps
	Title       string
	AcceptLabel string
	CancelLabel string
}

// ConfirmDialog renders a native <dialog>. Native, so the focus trap, the
// Escape key, the backdrop and the inertness of the page behind it are the
// browser's — the controller that opens it is twenty lines and traps nothing.
//
// There is one per page, and the message is written into it at the moment it
// opens, because a dialog per destructive button is a dialog per row.
func ConfirmDialog(p ConfirmDialogProps) g.Node {
	id := p.ID
	if id == "" {
		id = "pk-confirm"
	}
	props := p.ComponentProps
	props.ID = ""
	nodes := baseAttrs(props)
	nodes = append(nodes,
		h.ID(id),
		classes(clConfirmDialog.Compile(), p.Class),
		g.Attr("aria-labelledby", id+"-title"),
		h.H2(h.ID(id+"-title"), h.Class(clConfirmTitle.Compile()),
			g.Text(fallbackText(p.Title, "Are you sure?"))),
		h.P(h.Class(clConfirmMessage.Compile()), g.Attr("data-confirm-message")),
		h.Div(h.Class(clFormActions.Compile()),
			h.Button(h.Type("button"), g.Attr("data-confirm-cancel"),
				h.Class(clButtonBase.Merge(clButtonVariant["secondary"]).Merge(clButtonSize["md"]).Compile()),
				g.Attr("onclick", "this.closest('dialog').close()"),
				g.Text(fallbackText(p.CancelLabel, "Cancel"))),
			h.Button(h.Type("button"), g.Attr("data-confirm-accept"),
				h.Class(clButtonBase.Merge(clButtonTone["danger"]).Merge(clButtonSize["md"]).Compile()),
				g.Text(fallbackText(p.AcceptLabel, "Delete"))),
		),
	)
	return h.Dialog(nodes...)
}
