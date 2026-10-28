package forms

import (
	"strconv"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/components"
)

// The rich text field's chrome. Everything an author can click is drawn here, in
// the kernel's own components and words, and the browser only ever binds a
// behaviour to it: ui/assets/js/richtext.js mounts the editor on
// [data-richtext-surface], toggles the state attributes named here and positions
// the floating rows. With JavaScript gone the markup is inert text beside a
// textarea that still posts — the same form with less, not a control that lies
// about working.
//
// The vocabulary is closed: every attribute here is declared in ui/hooks.go, and
// every command name has a case in the glue. A command the server does not draw
// is not offered; a command the glue does not implement is not drawn.

// richtextCommand is one action of that chrome, drawn wherever it belongs. Title
// is the button's own word, Help the line the command menu shows beneath it, and
// Shortcut the keystroke the tooltip names so the tooltip teaches instead of
// merely labelling.
type richtextCommand struct {
	Command  string
	TitleKey string
	Title    string
	HelpKey  string
	Help     string
	Shortcut string
	// Toggle carries aria-pressed, Mark marks a bubble-only action, and Image
	// needs the file upload route to have been offered at all.
	Toggle, Bubble, Menu, Image bool
	// Toolbar lists the command on the fixed row; the first few are drawn at once
	// and the rest behind "More formatting" (see shownFirst).
	Toolbar bool
}

// richtextCommands is the whole surface, in the order the toolbar shows it. One
// list rather than twelve call sites, so the toolbar, the selection bubble and
// the command menu cannot disagree about what an author may do.
var richtextCommands = []richtextCommand{
	{Command: "bold", TitleKey: "forms.richtext.bold", Title: "Bold", Shortcut: "Ctrl+B", Toggle: true, Toolbar: true, Bubble: true},
	{Command: "italic", TitleKey: "forms.richtext.italic", Title: "Italic", Shortcut: "Ctrl+I", Toggle: true, Toolbar: true, Bubble: true},
	{Command: "link", TitleKey: "forms.richtext.link", Title: "Link", Shortcut: "Ctrl+K", Toggle: true, Toolbar: true, Bubble: true},
	{Command: "bulletList", TitleKey: "forms.richtext.bulletList", Title: "Bulleted list", HelpKey: "forms.richtext.menu.bulletList", Help: "Unordered list of items", Toggle: true, Toolbar: true, Menu: true},
	{Command: "orderedList", TitleKey: "forms.richtext.orderedList", Title: "Numbered list", HelpKey: "forms.richtext.menu.orderedList", Help: "List numbered from one", Toggle: true, Toolbar: true, Menu: true},
	{Command: "taskList", TitleKey: "forms.richtext.taskList", Title: "Task list", HelpKey: "forms.richtext.menu.taskList", Help: "Checkboxes you can tick off", Toggle: true, Toolbar: true, Menu: true},
	{Command: "quote", TitleKey: "forms.richtext.quote", Title: "Quote", HelpKey: "forms.richtext.menu.quote", Help: "A passage someone else wrote", Shortcut: "Ctrl+Shift+B", Toggle: true, Toolbar: true, Menu: true},
	{Command: "code", TitleKey: "forms.richtext.code", Title: "Inline code", Shortcut: "Ctrl+E", Toggle: true, Bubble: true},
	{Command: "strike", TitleKey: "forms.richtext.strike", Title: "Strikethrough", Toggle: true, Toolbar: true},
	{Command: "codeBlock", TitleKey: "forms.richtext.codeBlock", Title: "Code block", HelpKey: "forms.richtext.menu.codeBlock", Help: "Monospaced text, unformatted", Menu: true},
	{Command: "table", TitleKey: "forms.richtext.table", Title: "Table", HelpKey: "forms.richtext.menu.table", Help: "Rows and columns of short values", Menu: true},
	{Command: "heading2", TitleKey: "forms.richtext.heading2", Title: "Heading 2", HelpKey: "forms.richtext.menu.heading2", Help: "A section inside a section", Bubble: true, Menu: true},
	{Command: "heading3", TitleKey: "forms.richtext.heading3", Title: "Heading 3", HelpKey: "forms.richtext.menu.heading3", Help: "A sub-section heading", Menu: true},
	{Command: "paragraph", TitleKey: "forms.richtext.paragraph", Title: "Text", HelpKey: "forms.richtext.menu.paragraph", Help: "An ordinary paragraph", Bubble: true, Menu: true},
	{Command: "image", TitleKey: "forms.richtext.image", Title: "Image", HelpKey: "forms.richtext.menu.image", Help: "Upload a picture from this device, or paste one", Menu: true, Image: true},
	{Command: "rule", TitleKey: "forms.richtext.rule", Title: "Divider", HelpKey: "forms.richtext.menu.rule", Help: "A line across the page", Menu: true},
	{Command: "undo", TitleKey: "forms.richtext.undo", Title: "Undo", Shortcut: "Ctrl+Z", Toolbar: true},
	{Command: "redo", TitleKey: "forms.richtext.redo", Title: "Redo", Shortcut: "Ctrl+Shift+Z", Toolbar: true},
	{Command: "preview", TitleKey: "forms.richtext.preview", Title: "Preview as published", HelpKey: "forms.richtext.menu.preview", Help: "The page as the server renders it", Toggle: true, Toolbar: true},
}

// shownFirst names what the toolbar shows before anybody opens "More
// formatting": the marks and lists almost everyone uses, the image, and the pair
// that undoes. Everything else sits behind a control that says what it reveals.
func shownFirst(c richtextCommand) bool {
	switch c.Command {
	case "bold", "italic", "link", "bulletList", "orderedList", "quote", "image", "undo", "redo":
		return true
	}
	return false
}

// richtextChrome draws the editor's chrome for one field. area is the field's own
// textarea and stays the submitted value; it is drawn first, and every piece of
// chrome starts hidden, so a page with no JavaScript shows the textarea with the
// "Formatting help" disclosure below it and nothing that does nothing.
func richtextChrome(p ControlProps, area, help g.Node) g.Node {
	word := p.Text
	if word == nil {
		word = func(_, fallback string) string { return fallback }
	}

	attrs := richtextAttributes(p, word)
	// Read-only or disabled: the text, the counters and nothing focusable. The
	// frame is drawn anyway so the field is laid out as it is when editable.
	if p.Immutable {
		return h.Div(append(attrs, area, help)...)
	}

	toolbar := richtextToolbar(p, word)
	editor := p.Field.Editor
	menu := selectedRichtextCommands(editor, func(c richtextCommand) bool { return c.Menu })

	frame := append(attrs, area, h.Div(append(toolbar, h.Hidden(""))...))
	frame = append(frame,
		h.Div(
			h.Role("toolbar"), g.Attr("data-richtext-bubble", ""),
			h.Aria("label", word("forms.richtext.bubble", "Format selection")), h.Hidden(""),
			g.Group(richtextToolbarRow(word, selectedRichtextCommands(editor, func(c richtextCommand) bool { return c.Bubble })...)),
		),
		richtextBlockMenu(menu, word),
		richtextLinkControls(p.Field.Definition.Name, word),
		h.Div(g.Attr("data-richtext-surface", ""), h.Hidden("")),
		h.P(g.Attr("data-richtext-status", ""), h.Aria("live", "polite"), h.Hidden("")),
		h.P(g.Attr("data-richtext-count", ""), h.Hidden("")),
		h.Input(h.Type("file"), h.Accept("image/*"), h.Multiple(), g.Attr("data-richtext-file", ""), h.Hidden("")),
		richtextDraftControls(word),
		h.Div(g.Attr("data-richtext-preview", ""), h.Hidden("")),
		// The one slot a refusal, a conflict or a returned draft occupies. It is
		// drawn empty so the browser fills a slot the page laid out instead of
		// inventing a widget of its own.
		h.Div(g.Attr("data-richtext-offer", ""), h.Hidden("")),
		help,
	)
	return h.Div(frame...)
}

func richtextAttributes(p ControlProps, word func(string, string) string) []g.Node {
	f := p.Field.Definition
	editor := p.Field.Editor
	// Editor capabilities come from this request; absent attributes disable them.
	attrs := append([]g.Node{g.Attr("data-richtext", "1"), g.Attr("data-richtext-field", f.Name)}, anchorNodes(p.Field.Anchor)...)
	// Name the enhanced editing surface as well as the backing textarea.
	attrs = append(attrs, g.Attr("data-richtext-name", word("forms.richtext.surface", "Content")))
	for _, line := range richtextSentences {
		attrs = append(attrs, g.Attr(line.Attr, word(line.Key, line.Fallback)))
	}
	if f.MaxLength > 0 {
		attrs = append(attrs, g.Attr("data-richtext-max", strconv.Itoa(f.MaxLength)))
	}
	if editor.Endpoint != "" {
		attrs = append(attrs, g.Attr("data-richtext-endpoint", editor.Endpoint))
		if editor.ETag != "" {
			attrs = append(attrs, g.Attr("data-richtext-etag", editor.ETag))
		}
	}
	if editor.DraftKey != "" {
		attrs = append(attrs, g.Attr("data-richtext-draft", editor.DraftKey))
	}
	if editor.FileUpload != "" {
		attrs = append(attrs, g.Attr("data-richtext-upload", editor.FileUpload))
		if editor.ImageSrc != "" {
			attrs = append(attrs, g.Attr("data-richtext-image-src", editor.ImageSrc))
		}
		if editor.Limits != "" {
			attrs = append(attrs, g.Attr("data-richtext-limits", editor.Limits))
		}
	}
	// The one thing the browser cannot tell by looking: whether the text in front of
	// it is the record's or the person's own refused submission. A draft is kept
	// through a refused write, so the page that refused says so.
	if editor.Submitted {
		attrs = append(attrs, g.Attr("data-richtext-submitted", "1"))
	}
	return attrs
}

func richtextButton(c richtextCommand, word func(string, string) string) g.Node {
	marks := map[string]string{"data-richtext-command": c.Command}
	if c.Toggle {
		marks["aria-pressed"] = "false"
	}
	title := word(c.TitleKey, c.Title)
	name := title
	if c.Shortcut != "" {
		name = title + " (" + c.Shortcut + ")"
	}
	return components.Button(components.ButtonProps{
		ComponentProps: components.ComponentProps{Attrs: marks},
		Type:           "button", Label: title, AriaLabel: name, Variant: "ghost", Size: "sm",
	})
}

func richtextToolbarRow(word func(string, string) string, visible ...richtextCommand) []g.Node {
	nodes := make([]g.Node, 0, len(visible))
	for _, c := range visible {
		nodes = append(nodes, richtextButton(c, word))
	}
	return nodes
}

func selectedRichtextCommands(editor Editor, which func(richtextCommand) bool) []richtextCommand {
	var out []richtextCommand
	for _, c := range richtextCommands {
		if which(c) && !(c.Image && editor.FileUpload == "") {
			out = append(out, c)
		}
	}
	return out
}

func richtextToolbar(p ControlProps, word func(string, string) string) []g.Node {
	f := p.Field.Definition
	editor := p.Field.Editor
	first := selectedRichtextCommands(editor, func(c richtextCommand) bool { return c.Toolbar && shownFirst(c) })
	more := selectedRichtextCommands(editor, func(c richtextCommand) bool { return c.Toolbar && !shownFirst(c) })
	panelID := f.Name + "-richtext-more"
	toolbar := []g.Node{
		h.Role("toolbar"), g.Attr("data-richtext-toolbar", ""),
		h.Aria("label", word("forms.richtext.toolbar", "Formatting")),
		g.Group(richtextToolbarRow(word, first...)),
	}
	if len(more) > 0 {
		toolbar = append(toolbar,
			components.Button(components.ButtonProps{
				ComponentProps: components.ComponentProps{Attrs: map[string]string{
					"data-richtext-more": "", "aria-expanded": "false", "aria-controls": panelID,
				}},
				Type: "button", Label: word("forms.richtext.more", "More formatting"), Variant: "ghost", Size: "sm",
			}),
			h.Div(h.ID(panelID), g.Attr("data-richtext-more-panel", ""), h.Hidden(""), g.Group(richtextToolbarRow(word, more...))),
		)
	}
	return toolbar
}

func richtextBlockMenu(menu []richtextCommand, word func(string, string) string) g.Node {
	return h.Div(
		g.Attr("data-richtext-menu", ""), h.Role("listbox"), h.Hidden(""),
		h.Aria("label", word("forms.richtext.menu", "Insert a block")),
		// Two lines per row — the block's name and one line about it — because
		// the menu's job is to teach an author what a block is, not only what
		// it is called.
		g.Group(richtextMenuRows(menu, word)),
	)
}

func richtextLinkControls(name string, word func(string, string) string) g.Node {
	return h.Div(
		g.Attr("data-richtext-link", ""), h.Hidden(""),
		h.Label(h.For(name+"-richtext-link"), g.Text(word("forms.richtext.linkAddress", "Address"))),
		// Site-relative paths and fragments are valid links, so native URL
		// validation must not reject them before the rich-text policy runs.
		h.Input(h.Type("text"), h.ID(name+"-richtext-link"), g.Attr("inputmode", "url"), g.Attr("data-richtext-link-input", "")),
		components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-richtext-command": "link-save"}},
			Type:           "button", Label: word("forms.richtext.linkSave", "Save link"), Size: "sm",
		}),
		components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-richtext-command": "link-remove"}},
			Type:           "button", Label: word("forms.richtext.linkRemove", "Remove link"), Variant: "ghost", Size: "sm",
		}),
	)
}

func richtextDraftControls(word func(string, string) string) g.Node {
	return h.Div(
		g.Attr("data-richtext-draft-bar", ""), h.Hidden(""),
		h.P(g.Text(word("forms.richtext.draftFound", "A draft from this device was not saved."))),
		components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-richtext-draft-restore": ""}},
			Type:           "button", Label: word("forms.richtext.draftRestore", "Restore unsaved draft"), Size: "sm",
		}),
		components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-richtext-draft-discard": ""}},
			Type:           "button", Label: word("forms.richtext.draftDiscard", "Discard"), Variant: "ghost", Size: "sm",
		}),
	)
}

func richtextMenuRows(menu []richtextCommand, word func(string, string) string) []g.Node {
	rows := make([]g.Node, 0, len(menu))
	for _, c := range menu {
		rows = append(rows, h.Button(
			h.Type("button"), h.Role("option"), h.Aria("selected", "false"),
			g.Attr("data-richtext-command", c.Command),
			h.Span(g.Text(word(c.TitleKey, c.Title))),
			h.Span(g.Text(word(helpKey(c), c.Help))),
		))
	}
	return rows
}

// richtextSentences are the lines the browser writes while the person is writing:
// the counter, an upload's progress, a refused address, a conflict. Each is drawn
// as an attribute in the request's language so the glue holds no prose of its own —
// a sentence the page did not draw is a line the field leaves blank, never one the
// browser spells in whichever language its own install happens to use.
var richtextSentences = []struct {
	Attr, Key, Fallback string
}{
	{"data-richtext-words", "forms.richtext.words", "{count} words"},
	{"data-richtext-word-saving", "forms.richtext.word.saving", "Saving"},
	{"data-richtext-word-saved", "forms.richtext.word.saved", "Saved"},
	{"data-richtext-word-dirty", "forms.richtext.word.dirty", "Not saved yet"},
	{"data-richtext-word-drafted", "forms.richtext.word.drafted", "Kept on this device"},
	{"data-richtext-word-offline", "forms.richtext.word.offline", "Not saved — offline"},
	{"data-richtext-uploading", "forms.richtext.uploading", "Uploading"},
	{"data-richtext-failed", "forms.richtext.failed", "Upload failed"},
	{"data-richtext-retry", "forms.richtext.retry", "Retry"},
	{"data-richtext-choose", "forms.richtext.choose", "Choose another file"},
	{"data-richtext-refused", "forms.richtext.refused", "Not saved"},
	{"data-richtext-no-draft", "forms.richtext.noDraft", "This browser will not keep a draft"},
	{"data-richtext-bad-link", "forms.richtext.badLink", "That address is not a link this page may publish."},
	{"data-richtext-preview-note", "forms.richtext.previewNote", "The editor's own preview. Only the published page is the server's rendering."},
	{"data-richtext-draft-found", "forms.richtext.draftFound", "A draft from this device was not saved."},
	{"data-richtext-alt-title", "forms.richtext.altTitle", "Alt text"},
	{"data-richtext-alt-required", "forms.richtext.altRequired", "Describe the picture, or say it is decorative."},
	{"data-richtext-alt-gone", "forms.richtext.altGone", "This picture is no longer in the document."},
	{"data-richtext-decorative", "forms.richtext.decorative", "Decorative"},
	{"data-richtext-save", "forms.richtext.save", "Save"},
	{"data-richtext-choices", "forms.richtext.choices", "Choices"},
	{"data-richtext-conflict", "forms.richtext.conflict", "Changed by someone else — compare"},
	{"data-richtext-keep", "forms.richtext.keep", "Keep mine"},
	{"data-richtext-theirs", "forms.richtext.theirs", "Take theirs"},
	{"data-richtext-merge", "forms.richtext.merge", "Merge by hand"},
}

// helpKey is the catalogue key of a command menu's description, which a command
// that never appears in the menu does not carry.
func helpKey(c richtextCommand) string {
	if c.HelpKey == "" {
		return c.TitleKey
	}
	return c.HelpKey
}
