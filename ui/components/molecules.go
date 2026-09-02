package components

// go renders the molecule contracts the admin and module pages
// compose: tabular data, cards, navigation, and progressively enhanced
// overlays. Interaction contracts stay in the shared runtime controllers so
// downstream products do not need private scripts or duplicate markup.

import (
	"fmt"
	"net/url"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/style"
)

func defaultTrue(value *bool) bool {
	return value == nil || *value
}

// ModalSlots is the trusted Go composition seam for rich dialog regions.
// Portable clients use ModalProps body/footer strings; server-rendered Go
// applications use these slots without recreating modal chrome or behavior.
type ModalSlots struct {
	Header []g.Node
	Body   []g.Node
	Footer []g.Node
}

// Modal renders a portable modal contract.
func Modal(p ModalProps) g.Node {
	return ModalWithSlots(p, ModalSlots{})
}

// ModalWithSlots renders the canonical modal root. Deferred roots deliberately
// contain no panel: HTMX swaps a ModalPanelWithSlots response into the root and
// the shared htmx-modal controller opens it after the swap.
func ModalWithSlots(p ModalProps, slots ModalSlots) g.Node {
	size := modalSize(p.Size)
	open := p.Open && !p.Hidden
	state := "closed"
	if open {
		state = "open"
	}
	centered := defaultTrue(p.Centered)
	closeOnEscape := defaultTrue(p.CloseOnEscape)
	clearOnClose := p.Deferred
	if p.ClearOnClose != nil {
		clearOnClose = *p.ClearOnClose
	}

	rootClass := clModalRoot.Merge(clModalCentered)
	if !centered {
		rootClass = clModalRoot.Merge(clModalBottomSheet)
	}
	rootProps := p.ComponentProps
	rootProps.Class = ""
	rootProps.Disabled = false
	rootProps.Hidden = false
	root := baseAttrs(rootProps)
	root = append(root,
		classes(rootClass.Compile(), p.Class),
		g.Attr("data-component", "modal"),
		g.Attr("data-controller", "htmx-modal"),
		g.Attr("data-htmx-modal-open-value", boolText(open)),
		g.Attr("data-htmx-modal-close-on-escape-value", boolText(closeOnEscape)),
		g.Attr("data-htmx-modal-clear-on-close-value", boolText(clearOnClose)),
		g.Attr("data-state", state),
		g.Attr("aria-hidden", boolText(!open)),
		g.Attr("tabindex", "-1"),
	)
	if p.Disabled {
		root = append(root, g.Attr("aria-disabled", "true"))
	}
	if !open {
		root = append(root, g.Attr("hidden"), h.Style("display:none"))
	}

	actions := make([]string, 0, 2)
	if p.OpenOnSwap {
		actions = append(actions, "htmx:afterSwap->htmx-modal#show")
	}
	if p.Deferred && defaultTrue(p.CloseOnOverlay) && !p.Disabled {
		actions = append(actions, "click->htmx-modal#backdropClick")
	}
	if len(actions) > 0 {
		root = append(root, g.Attr("data-action", strings.Join(actions, " ")))
	}
	if p.Deferred {
		return h.Div(root...)
	}

	root = append(root, h.Role("dialog"), g.Attr("aria-modal", "true"))
	titleID := modalTitleID(p, slots)
	accessibleName := modalAccessibleName(p)
	if titleID != "" && strings.TrimSpace(p.AriaLabel) == "" {
		root = append(root, g.Attr("aria-labelledby", titleID))
	} else {
		root = append(root, g.Attr("aria-label", accessibleName))
	}
	if defaultTrue(p.ShowOverlay) {
		overlay := []g.Node{
			h.Class(clModalOverlay.Compile()),
			g.Attr("data-modal-backdrop", ""),
			g.Attr("aria-hidden", "true"),
		}
		if defaultTrue(p.CloseOnOverlay) && !p.Disabled {
			overlay = append(overlay, g.Attr("data-action", "click->htmx-modal#close"))
		}
		root = append(root, h.Div(overlay...))
	}
	root = append(root, modalPanel(p, slots, false, size))
	return h.Div(root...)
}

// ModalPanelWithSlots renders the panel fragment returned by an HTMX endpoint.
// It is a complete dialog because its deferred parent is only a swap target.
func ModalPanelWithSlots(p ModalProps, slots ModalSlots) g.Node {
	return modalPanel(p, slots, true, modalSize(p.Size))
}

// ModalPanel renders a server-loaded modal panel with one rich body node.
func ModalPanel(p ModalProps, body g.Node) g.Node {
	return ModalPanelWithSlots(p, ModalSlots{Body: []g.Node{body}})
}

func modalPanel(p ModalProps, slots ModalSlots, standalone bool, size string) g.Node {
	closable := defaultTrue(p.Closable)
	showClose := defaultTrue(p.ShowClose)
	titleID := modalTitleID(p, slots)
	closeLabel := strings.TrimSpace(p.CloseLabel)
	if closeLabel == "" {
		closeLabel = "Close"
	}

	headerContent := slots.Header
	if len(headerContent) == 0 && (p.Title != "" || p.Description != "") {
		text := make([]g.Node, 0, 2)
		if p.Title != "" {
			title := []g.Node{h.Class(clModalTitle.Compile()), g.Text(p.Title)}
			if titleID != "" {
				title = append([]g.Node{h.ID(titleID)}, title...)
			}
			text = append(text, h.H2(title...))
		}
		if p.Description != "" {
			text = append(text, h.P(h.Class(clModalDescription.Compile()), g.Text(p.Description)))
		}
		headerContent = []g.Node{h.Div(h.Class(clModalTitleBlock.Compile()), g.Group(text))}
	}
	header := append([]g.Node(nil), headerContent...)
	if closable && showClose {
		header = append(header, ModalCloseButton(closeLabel, ""))
	}

	body := slots.Body
	if len(body) == 0 && p.Body != "" {
		body = []g.Node{g.Text(p.Body)}
	}
	footer := slots.Footer
	if len(footer) == 0 && p.Footer != "" {
		footer = []g.Node{g.Text(p.Footer)}
	}

	panel := []g.Node{
		h.Class(clModalPanel.Merge(clModalPanelSize[size]).Compile()),
		g.Attr("data-modal-panel", ""),
		g.Attr("data-action", "click->htmx-modal#stopPropagation"),
		g.Attr("tabindex", "-1"),
	}
	if standalone {
		panel = append(panel, h.Role("dialog"), g.Attr("aria-modal", "true"))
		if titleID != "" && strings.TrimSpace(p.AriaLabel) == "" {
			panel = append(panel, g.Attr("aria-labelledby", titleID))
		} else {
			panel = append(panel, g.Attr("aria-label", modalAccessibleName(p)))
		}
	}
	if len(header) > 0 {
		panel = append(panel, h.Div(h.Class(clModalHeader.Compile()), g.Group(header)))
		panel = append(panel, modalSeparator("header"))
	}
	panel = append(panel, h.Div(
		h.Class(clModalBody.Compile()),
		g.Attr("data-modal-body", ""),
		g.Group(body),
	))
	if len(footer) > 0 {
		panel = append(panel, modalSeparator("footer"))
		panel = append(panel, h.Div(
			h.Class(clModalFooter.Compile()),
			g.Attr("data-modal-footer", ""),
			g.Group(footer),
		))
	}
	return h.Div(panel...)
}

// modalSeparator is explicit structure rather than a side-border utility so
// browser and native design renderers receive the same one-pixel boundary.
func modalSeparator(section string) g.Node {
	return h.Div(
		h.Class(clModalSeparator.Compile()),
		g.Attr("data-modal-separator", section),
		g.Attr("aria-hidden", "true"),
	)
}

func modalSize(value string) string {
	size := strings.ToLower(strings.TrimSpace(value))
	if size == "fullscreen" {
		size = "full"
	}
	if _, ok := clModalPanelSize[size]; !ok {
		return "medium"
	}
	return size
}

func modalTitleID(p ModalProps, slots ModalSlots) string {
	if p.ID == "" || p.Title == "" || len(slots.Header) > 0 {
		return ""
	}
	return p.ID + "-title"
}

func modalAccessibleName(p ModalProps) string {
	if label := strings.TrimSpace(p.AriaLabel); label != "" {
		return label
	}
	if title := strings.TrimSpace(p.Title); title != "" {
		return title
	}
	return "Dialog"
}

// ModalCloseButton creates the canonical controller-backed icon close action.
func ModalCloseButton(label, class string) g.Node {
	if strings.TrimSpace(label) == "" {
		label = "Close"
	}
	return h.Button(
		h.Type("button"), classes(clModalClose.Compile(), class),
		g.Attr("data-action", "click->htmx-modal#close"),
		g.Attr("data-modal-close", ""), g.Attr("aria-label", label),
		Icon(IconProps{Name: "x", Size: "md"}),
	)
}

// ModalCancelButton creates a text dismissal action for modal footers.
func ModalCancelButton(label, class string) g.Node {
	if strings.TrimSpace(label) == "" {
		label = "Cancel"
	}
	return h.Button(
		h.Type("button"), classes(clModalCancel.Compile(), class),
		g.Attr("data-action", "click->htmx-modal#close"),
		g.Attr("data-modal-cancel", ""), g.Text(label),
	)
}

// ModalForm closes its owning modal after a successful HTMX request.
func ModalForm(attrs ...g.Node) g.Node {
	nodes := make([]g.Node, 0, len(attrs)+1)
	nodes = append(nodes, attrs...)
	nodes = append(nodes, g.Attr("data-action", "htmx:afterRequest->htmx-modal#closeOnSuccess"))
	return h.Form(nodes...)
}

// DetailList renders a governed group of label/value facts as a native
// description list. SemanticRole is intentionally projected only as a data
// attribute: it is an adaptive-surface machine key, not an HTML/ARIA role or
// translated label.
func DetailList(p DetailListProps) g.Node {
	rootProps := p.ComponentProps
	rootProps.Class = ""
	rootProps.Disabled = false
	root := baseAttrs(
		rootProps,
		classes(clDetailList.Compile(), p.Class),
		g.Attr("data-component", "detail-list"),
	)
	if p.Disabled {
		root = append(root, g.Attr("aria-disabled", "true"))
	}
	if semanticRole := validDetailSemanticRole(p.SemanticRole); semanticRole != "" {
		root = append(root, g.Attr("data-semantic-role", semanticRole))
	}

	headingID := ""
	descriptionID := ""
	hasTitle := strings.TrimSpace(p.Title) != ""
	hasDescription := strings.TrimSpace(p.Description) != ""
	if id := strings.TrimSpace(p.ID); id != "" {
		if hasTitle {
			headingID = id + "-title"
			root = append(root, g.Attr("aria-labelledby", headingID))
		}
		if hasDescription {
			descriptionID = id + "-description"
			root = append(root, g.Attr("aria-describedby", descriptionID))
		}
	}
	if hasTitle || hasDescription {
		header := []g.Node{h.Class(clDetailHeader.Compile())}
		if hasTitle {
			title := []g.Node{h.Class(clDetailTitle.Compile())}
			if headingID != "" {
				title = append(title, h.ID(headingID))
			}
			title = append(title, g.Text(p.Title))
			header = append(header, h.H2(title...))
		}
		if hasDescription {
			description := []g.Node{h.Class(clDetailDescription.Compile())}
			if descriptionID != "" {
				description = append(description, h.ID(descriptionID))
			}
			description = append(description, g.Text(p.Description))
			header = append(header, h.P(description...))
		}
		root = append(root, h.Header(header...))
	}

	items := make([]g.Node, 0, len(p.Items)+1)
	items = append(items, h.Class(clDetailItems.Compile()))
	validItems := 0
	for _, item := range p.Items {
		if strings.TrimSpace(item.Label) == "" || strings.TrimSpace(item.Value) == "" {
			continue
		}
		rowClass := clDetailRow
		if validItems > 0 {
			rowClass = rowClass.Merge(clDetailRowSeparated)
		}
		tone := strings.ToLower(strings.TrimSpace(item.Tone))
		if _, ok := clDetailValueTone[tone]; !ok {
			tone = "neutral"
		}
		term := []g.Node{h.Class(clDetailTerm.Compile()), g.Text(item.Label)}
		if strings.TrimSpace(item.Description) != "" {
			term = append(term, h.Span(
				h.Class(clDetailTermDescription.Compile()),
				g.Text(item.Description),
			))
		}
		items = append(items, h.Div(
			h.Class(rowClass.Compile()),
			g.Attr("data-detail-item", ""),
			h.Dt(term...),
			h.Dd(
				h.Class(clDetailValue.Merge(clDetailValueTone[tone]).Compile()),
				g.Text(item.Value),
			),
		))
		validItems++
	}
	if validItems > 0 {
		root = append(root, h.Dl(items...))
	}
	return h.Section(root...)
}

// validDetailSemanticRole accepts portable machine keys while failing closed
// for display copy, whitespace, URLs, and other values that should never be
// used to drive renderer behavior. Recommended shared keys include identity,
// preferences, and activity; namespaced extensions remain possible.
func validDetailSemanticRole(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 64 {
		return ""
	}
	for index, char := range value {
		if index == 0 {
			if char < 'a' || char > 'z' {
				return ""
			}
			continue
		}
		if (char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_' || char == '.' {
			continue
		}
		return ""
	}
	return value
}

// TableSlots is the trusted Go composition seam for rich web cells and
// server-driven sorting. Portable table data remains in TableProps; callers
// only opt into these callbacks when a cell needs real markup.
type TableSlots struct {
	Cell             func(TableRow, TableColumn) g.Node
	CellAttrs        func(TableRow, TableColumn) []g.Node
	RowAttrs         func(TableRow) []g.Node
	SortURL          func(TableColumn) string
	SortState        func(TableColumn) string
	SortButtonAttrs  func(TableColumn) []g.Node
	SelectAllLabel   string
	SelectRowLabel   func(TableRow) string
	SelectRowChecked func(TableRow) bool
}

// Table renders TableProps. Cell values render via fmt.Sprint;
// rows are keyed by column order. An empty Rows slice renders EmptyText.
func Table(p TableProps) g.Node {
	return TableWithSlots(p, TableSlots{})
}

// TableWithSlots renders the canonical table while allowing trusted Go
// composition to project rich cell nodes without creating a second table
// renderer.
func TableWithSlots(p TableProps, slots TableSlots) g.Node {
	sortable := func(c TableColumn) bool { return p.Sortable && c.Sortable }

	head := []g.Node{h.Class(clTableHead.Compile())}
	var headCells []g.Node
	if p.Selectable {
		label := fallbackText(slots.SelectAllLabel, "Select all rows")
		headCells = append(headCells, h.Th(
			h.Class(clTableTh.Compile()), g.Attr("scope", "col"),
			h.Input(h.Class(clCheckbox.Compile()), h.Type("checkbox"),
				g.Attr("data-pk-select", "all"), g.Attr("aria-label", label)),
		))
	}
	for _, c := range p.Columns {
		if sortable(c) {
			sortState := "none"
			if slots.SortState != nil {
				switch state := slots.SortState(c); state {
				case "ascending", "descending":
					sortState = state
				}
			}
			glyph := "↕"
			switch sortState {
			case "ascending":
				glyph = "↑"
			case "descending":
				glyph = "↓"
			}
			button := []g.Node{
				h.Class(clTableSortBtn.Compile()), h.Type("button"),
				g.Attr("data-pk-sort", c.Key),
				g.Text(c.Label),
				h.Span(g.Attr("aria-hidden", "true"), g.Attr("data-pk-sort-icon", ""), g.Text(glyph)),
			}
			if slots.SortURL != nil {
				if sortURL := slots.SortURL(c); sortURL != "" {
					enhancement := p.HTMXProps
					enhancement.Get = sortURL
					enhancement.Trigger = ""
					button = append(button, htmxAttrs(enhancement)...)
				}
			}
			if slots.SortButtonAttrs != nil {
				button = append(button, slots.SortButtonAttrs(c)...)
			}
			// A real button inside the th: keyboard operable, and the page
			// script owns cycling aria-sort none → ascending → descending.
			headCells = append(headCells, h.Th(
				h.Class(clTableThSort.Compile()), g.Attr("scope", "col"),
				g.Attr("aria-sort", sortState),
				g.If(c.Width != "", g.Attr("style", "width:"+c.Width)),
				h.Button(button...),
			))
			continue
		}
		cell := []g.Node{h.Class(clTableTh.Compile()), g.Attr("scope", "col")}
		if c.Width != "" {
			cell = append(cell, g.Attr("style", "width:"+c.Width))
		}
		headCells = append(headCells, h.Th(append(cell, g.Text(c.Label))...))
	}
	head = append(head, h.Tr(headCells...))

	tdClass := clTableTd
	if p.Compact {
		tdClass = clTableTdC
	}
	var bodyRows []g.Node
	for i, r := range p.Rows {
		rowClass := clTableRow
		if p.Striped && i%2 == 1 {
			rowClass = clTableRow.Merge(clTableRowAlt)
		}
		cells := []g.Node{h.Class(rowClass.Compile())}
		if r.ID != "" {
			cells = append(cells, g.Attr("data-pk-row", r.ID))
		}
		if slots.RowAttrs != nil {
			cells = append(cells, slots.RowAttrs(r)...)
		}
		if p.Selectable {
			label := "Select row"
			if slots.SelectRowLabel != nil {
				label = fallbackText(slots.SelectRowLabel(r), label)
			}
			input := []g.Node{h.Class(clCheckbox.Compile()), h.Type("checkbox"),
				g.Attr("data-pk-select", r.ID), g.Attr("aria-label", label)}
			if slots.SelectRowChecked != nil && slots.SelectRowChecked(r) {
				input = append(input, h.Checked())
			}
			cells = append(cells, h.Td(h.Class(tdClass.Compile()), h.Input(input...)))
		}
		for _, c := range p.Columns {
			v := ""
			if raw, ok := r.Cells[c.Key]; ok && raw != nil {
				v = fmt.Sprint(raw)
			}
			cell := tdClass
			if c.Primary {
				cell = cell.Merge(clTableTdStrong)
			}
			td := []g.Node{h.Class(cell.Compile())}
			if slots.CellAttrs != nil {
				td = append(td, slots.CellAttrs(r, c)...)
			}
			switch c.Align {
			case "center":
				td = append(td, g.Attr("style", "text-align:center"))
			case "right":
				td = append(td, g.Attr("style", "text-align:right"))
			}
			content := g.Node(g.Text(v))
			if slots.Cell != nil {
				if rich := slots.Cell(r, c); rich != nil {
					content = rich
				}
			}
			bodyCell := h.Td(append(td, content)...)
			cells = append(cells, bodyCell)
		}
		bodyRows = append(bodyRows, h.Tr(cells...))
	}
	if len(bodyRows) == 0 {
		empty := p.EmptyText
		if empty == "" {
			empty = "Nothing to show yet."
		}
		span := len(p.Columns)
		if p.Selectable {
			span++
		}
		bodyRows = append(bodyRows, h.Tr(h.Td(
			h.Class(clTableTd.Compile()), h.ColSpan(itoa(span)),
			h.Class(clHelp.Compile()), g.Text(empty),
		)))
	}

	wrap := baseAttrs(p.ComponentProps, htmxAttrs(p.HTMXProps)...)
	wrap = append(wrap,
		classes(clTableWrap.Compile(), p.Class),
		g.Attr("data-component", "table"),
		h.Table(
			h.Class(clTable.Compile()),
			h.THead(head...),
			h.TBody(bodyRows...),
		),
	)
	return h.Div(wrap...)
}

// CardSlots is the trusted Go composition seam for the three structural card
// regions. Portable title/description/media data remains in CardProps.
type CardSlots struct {
	Header  []g.Node
	Content []g.Node
	Footer  []g.Node
}

// Card renders free-form card children. Call CardWithSlots when header,
// content, and footer need the canonical section treatment.
func Card(p CardProps, children ...g.Node) g.Node {
	return cardNode(p, CardSlots{}, children)
}

// CardWithSlots renders canonical header/content/footer regions without a
// downstream wrapper or style implementation.
func CardWithSlots(p CardProps, slots CardSlots) g.Node {
	return cardNode(p, slots, nil)
}

func cardNode(p CardProps, slots CardSlots, children []g.Node) g.Node {
	sectioned := len(slots.Header)+len(slots.Content)+len(slots.Footer) > 0
	rootClass := cardRootClasses(p, sectioned)
	nodes := baseAttrs(p.ComponentProps, htmxAttrs(p.HTMXProps)...)
	nodes = append(nodes,
		classes(rootClass, p.Class),
		g.Attr("data-component", "card"),
	)

	body := make([]g.Node, 0, 5)
	if p.Image != "" && normalizedCardImagePosition(p.ImagePosition) == "top" {
		body = append(body, cardImage(p, false))
	}
	if sectioned {
		padding := cardPaddingClasses(p.Padding, true)
		header := slots.Header
		if len(header) == 0 && (p.Title != "" || p.Description != "") {
			header = cardTextHeader(p)
		}
		if len(header) > 0 {
			body = append(body, h.Div(
				h.Class(strings.TrimSpace(padding+" "+clCardHeader.Compile())),
				g.Group(header),
			))
		}
		if len(slots.Content) > 0 {
			body = append(body, h.Div(h.Class(padding), g.Group(slots.Content)))
		}
		if len(slots.Footer) > 0 {
			body = append(body, h.Div(
				h.Class(strings.TrimSpace(padding+" "+clCardFooter.Compile())),
				g.Group(slots.Footer),
			))
		}
	} else {
		body = append(body, cardTextHeader(p)...)
		body = append(body, children...)
	}
	if p.Image != "" && normalizedCardImagePosition(p.ImagePosition) == "bottom" {
		body = append(body, cardImage(p, false))
	}

	position := normalizedCardImagePosition(p.ImagePosition)
	if p.Image != "" && (position == "left" || position == "right") {
		content := h.Div(h.Class(clCardVertical.Compile()), g.Group(body))
		image := cardImage(p, true)
		if position == "left" {
			body = []g.Node{h.Div(h.Class(clCardHorizontal.Compile()), image, content)}
		} else {
			body = []g.Node{h.Div(h.Class(clCardHorizontal.Compile()), content, image)}
		}
	}

	nodes = append(nodes, body...)
	if p.Clickable && p.Href != "" {
		return h.A(append(nodes, h.Href(p.Href))...)
	}
	return h.Article(nodes...)
}

func cardTextHeader(p CardProps) []g.Node {
	var nodes []g.Node
	if p.Title != "" {
		nodes = append(nodes, h.P(h.Class(clCardTitle.Compile()), g.Text(p.Title)))
	}
	if p.Description != "" {
		nodes = append(nodes, h.P(h.Class(clCardDesc.Compile()), g.Text(p.Description)))
	}
	return nodes
}

func cardImage(p CardProps, horizontal bool) g.Node {
	className := clCardImageVertical.Compile()
	if horizontal {
		className = clCardImageHorizontal.Compile()
	}
	return h.Img(h.Src(p.Image), h.Alt(p.ImageAlt), h.Class(className))
}

func normalizedCardImagePosition(position string) string {
	switch strings.ToLower(strings.TrimSpace(position)) {
	case "bottom", "left", "right":
		return strings.ToLower(strings.TrimSpace(position))
	default:
		return "top"
	}
}

func cardPaddingClasses(padding string, sectioned bool) string {
	switch strings.ToLower(strings.TrimSpace(padding)) {
	case "none":
		return clCardPadNone.Compile()
	case "small":
		return clCardPadSmall.Compile()
	case "large":
		return clCardPadLarge.Compile()
	case "medium":
		return clCardPadMedium.Compile()
	default:
		if sectioned {
			return clCardPadMedium.Compile()
		}
		return clCardPadDefault.Compile()
	}
}

func cardRootClasses(p CardProps, sectioned bool) string {
	cl := clCardFrame
	if sectioned {
		cl = cl.Merge(clCardSectioned)
	} else {
		switch strings.ToLower(strings.TrimSpace(p.Padding)) {
		case "none":
			cl = cl.Merge(clCardPadNone)
		case "small":
			cl = cl.Merge(clCardPadSmall)
		case "medium":
			cl = cl.Merge(clCardPadMedium)
		case "large":
			cl = cl.Merge(clCardPadLarge)
		default:
			cl = cl.Merge(clCardPadDefault)
		}
	}

	variant := strings.ToLower(strings.TrimSpace(p.Variant))
	if variant != "elevated" && variant != "plain" {
		cl = cl.Merge(clCardBorder)
	}
	shadow := strings.ToLower(strings.TrimSpace(p.Shadow))
	if shadow == "" {
		switch variant {
		case "outlined", "plain":
			shadow = "none"
		case "elevated":
			shadow = "medium"
		default:
			shadow = "small"
		}
	}
	switch shadow {
	case "medium":
		cl = cl.Merge(clCardShadowMedium)
	case "large":
		cl = cl.Merge(clCardShadowLarge)
	case "none":
	default:
		cl = cl.Merge(clCardShadowSmall)
	}
	if p.Hoverable {
		cl = cl.Merge(clCardHoverable)
	}
	if p.Clickable {
		cl = cl.Merge(clCardClickable)
	}
	return cl.Compile()
}

// SidebarSlots carries trusted rich composition around the portable navigation
// model. Items is a compatibility seam for already-rendered navigation nodes;
// new portable callers should prefer SidebarProps.Items or Sections.
type SidebarSlots struct {
	Brand  []g.Node
	Items  []g.Node
	Footer []g.Node
}

// Sidebar renders the portable sidebar model.
func Sidebar(p SidebarProps) g.Node {
	return SidebarWithSlots(p, SidebarSlots{})
}

// SidebarWithSlots renders the canonical persistent navigation surface while
// preserving rich brand and footer composition for trusted Go callers.
func SidebarWithSlots(p SidebarProps, slots SidebarSlots) g.Node {
	flavor := "admin"
	if p.Flavor == "content" {
		flavor = "content"
	}
	collapsible := p.Collapsible || p.Collapsed

	rootClass := clSidebarRootAdmin
	columnClass := clSidebarColumnAdmin
	brandClass := clSidebarBrandAdmin
	navWrapClass := clSidebarNavWrapAdmin
	navClass := clSidebarNavAdmin
	footerClass := clSidebarFooterAdmin
	if flavor == "content" {
		rootClass = clSidebarRootContent
		columnClass = clSidebarColumnContent
		brandClass = clSidebarBrandContent
		navWrapClass = clSidebarNavWrapContent
		navClass = clSidebarNavContent
	} else if p.Collapsed {
		rootClass = rootClass.Merge(clSidebarWidthCollapsed)
	} else {
		rootClass = rootClass.Merge(clSidebarWidthExpanded)
	}
	if flavor == "content" {
		footerClass = clSidebarFooterContent
	}
	if p.Disabled {
		rootClass = rootClass.Merge(clSidebarDisabled)
	}

	sidebarID := strings.TrimSpace(p.ID)
	if sidebarID == "" {
		sidebarID = flavor + "-sidebar"
	}
	if collapsible && !strings.HasSuffix(sidebarID, "-collapsible") {
		sidebarID += "-collapsible"
	}
	label := strings.TrimSpace(p.NavigationLabel)
	if label == "" {
		if flavor == "content" {
			label = "Content navigation"
		} else {
			label = "Admin navigation"
		}
	}

	rootProps := p.ComponentProps
	rootProps.ID = ""
	rootProps.Disabled = false
	attrs := append(baseAttrs(rootProps),
		h.ID(sidebarID),
		classes(rootClass.Compile(), p.Class),
		g.Attr("data-component", "sidebar"),
		g.Attr("data-sidebar-flavor", flavor),
		g.Attr("data-sidebar-collapsible", boolText(collapsible)),
		g.Attr("data-sidebar-collapsed", boolText(p.Collapsed)),
		g.Attr("data-state", sidebarState(p.Collapsed)),
		g.Attr("aria-disabled", boolText(p.Disabled)),
	)
	if collapsible {
		attrs = append(attrs, g.Attr("aria-expanded", boolText(!p.Collapsed)))
	}

	column := []g.Node{h.Class(columnClass.Compile())}
	if brand := sidebarBrand(p, slots.Brand, flavor, brandClass); brand != nil {
		column = append(column, brand)
	}
	column = append(column, h.Div(
		h.Class(navWrapClass.Compile()),
		h.Nav(
			h.Class(navClass.Compile()),
			g.Attr("aria-label", label),
			sidebarNavigation(p, slots.Items, flavor),
		),
	))
	if len(slots.Footer) > 0 {
		column = append(column, h.Footer(
			h.Class(footerClass.Compile()),
			g.Attr("data-sidebar-footer", ""),
			g.Group(slots.Footer),
		))
	}

	attrs = append(attrs, h.Div(
		h.Class(clSidebarInner.Compile()),
		h.Div(column...),
	))
	return h.Aside(attrs...)
}

func sidebarState(collapsed bool) string {
	if collapsed {
		return "collapsed"
	}
	return "expanded"
}

func sidebarBrand(
	p SidebarProps,
	brand []g.Node,
	flavor string,
	class style.ClassList,
) g.Node {
	if len(brand) > 0 {
		return h.Header(
			h.Class(class.Compile()),
			g.Attr("data-sidebar-brand", ""),
			g.Group(brand),
		)
	}
	label := strings.TrimSpace(p.BrandLabel)
	if label == "" && flavor == "admin" {
		label = "Admin"
	}
	if label == "" {
		return nil
	}
	href := strings.TrimSpace(p.BrandHref)
	if href == "" {
		href = "/admin"
	}
	return h.Header(
		h.Class(class.Compile()),
		g.Attr("data-sidebar-brand", ""),
		h.A(
			h.Href(href),
			h.Class(clSidebarBrandLink.Compile()),
			h.Span(h.Class(clSidebarBrandText.Compile()), g.Text(label)),
		),
	)
}

func sidebarNavigation(p SidebarProps, richItems []g.Node, flavor string) g.Node {
	if len(richItems) > 0 {
		return g.Group(richItems)
	}
	if len(p.Sections) > 0 {
		sections := make([]g.Node, 0, len(p.Sections))
		for _, section := range p.Sections {
			sections = append(sections, sidebarSectionNode(p, section, flavor))
		}
		return g.Group(sections)
	}
	items := make([]g.Node, 0, len(p.Items))
	for _, item := range p.Items {
		items = append(items, sidebarItemNode(p, item, flavor, 0))
	}
	return h.Ul(h.Class(clSidebarSectionList.Compile()), g.Group(items))
}

func sidebarSectionNode(
	p SidebarProps,
	section SidebarSection,
	flavor string,
) g.Node {
	sectionID := sidebarResolvedID(section.ID, section.Label)
	attrs := []g.Node{
		h.Class(clSidebarSection.Compile()),
		g.Attr("data-sidebar-section", sectionID),
		g.Attr("data-sidebar-search-section", "true"),
	}
	if tone := strings.TrimSpace(section.Tone); tone != "" {
		attrs = append(attrs, g.Attr("data-sidebar-tone", tone))
	}
	if searchText := strings.TrimSpace(section.SearchText); searchText != "" {
		attrs = append(attrs, g.Attr("data-sidebar-search-text", searchText))
	}
	if section.Label != "" || section.Glyph != "" {
		headerClass := clSidebarSectionHeaderAdmin
		if flavor == "content" {
			headerClass = clSidebarSectionHeaderContent
		}
		header := []g.Node{
			h.Class(headerClass.Compile()),
			g.Attr("data-sidebar-section-header", ""),
		}
		if section.Glyph != "" {
			header = append(header, h.Span(
				h.Class(clSidebarSectionGlyph.Compile()),
				g.Attr("data-sidebar-section-glyph", "true"),
				g.Attr("aria-hidden", "true"),
				g.Text(section.Glyph),
			))
		}
		if section.Label != "" {
			header = append(header, h.Span(
				g.Attr("data-sidebar-section-label", "true"),
				g.Text(section.Label),
			))
		}
		attrs = append(attrs, h.Div(header...))
	}
	items := make([]g.Node, 0, len(section.Items))
	for _, item := range section.Items {
		items = append(items, sidebarItemNode(p, item, flavor, 0))
	}
	attrs = append(attrs, h.Ul(h.Class(clSidebarSectionList.Compile()), g.Group(items)))
	return h.Section(attrs...)
}

func sidebarItemNode(
	p SidebarProps,
	item SidebarItem,
	flavor string,
	depth int,
) g.Node {
	active := sidebarItemActive(item, p.Current)
	disabled := p.Disabled || item.Disabled
	itemID := sidebarResolvedID(item.ID, item.Label)
	attrs := []g.Node{
		g.Attr("data-sidebar-item", itemID),
		g.Attr("data-sidebar-depth", itoa(depth)),
		g.Attr("data-active", boolText(active)),
		g.Attr("data-has-badge", boolText(item.Badge != "")),
	}
	if active {
		attrs = append(attrs, g.Attr("data-state", "active"))
	} else {
		attrs = append(attrs, g.Attr("data-state", "idle"))
	}
	if disabled {
		attrs = append(attrs, g.Attr("data-disabled", "true"))
	}
	if searchText := strings.TrimSpace(item.SearchText); searchText != "" {
		attrs = append(attrs,
			g.Attr("data-sidebar-search-item", "true"),
			g.Attr("data-sidebar-search-text", searchText),
		)
	}
	attrs = append(attrs, attrPairs(item.Attrs)...)

	linkClass := clSidebarLinkAdmin
	activeClass := clSidebarLinkActiveAdmin
	idleClass := clSidebarLinkIdleAdmin
	prefixClass := clSidebarPrefixAdmin
	labelClass := clSidebarLabelVisible
	if flavor == "content" {
		linkClass = clSidebarLinkContent
		activeClass = clSidebarLinkActiveContent
		idleClass = clSidebarLinkIdleContent
		prefixClass = clSidebarPrefixContent
		labelClass = clSidebarLabelContent
	}
	if p.Collapsed && flavor == "admin" {
		linkClass = linkClass.Merge(clSidebarLinkPadCollapsed)
		labelClass = clSidebarLabelHidden
	} else {
		linkClass = linkClass.Merge(clSidebarLinkPadExpanded)
	}
	if active {
		linkClass = linkClass.Merge(activeClass)
	} else {
		linkClass = linkClass.Merge(idleClass)
	}
	if disabled {
		linkClass = linkClass.Merge(clSidebarItemDisabled)
	}

	content := []g.Node{h.Class(linkClass.Compile()), g.Attr("data-nav", itemID)}
	if active {
		content = append(content, g.Attr("aria-current", "page"))
	}
	if disabled {
		content = append(content, g.Attr("aria-disabled", "true"))
	}
	if p.Collapsed && flavor == "admin" {
		content = append(content, g.Attr("aria-label", item.Label))
	}
	if validIconName(item.Icon) {
		content = append(content, h.Span(
			g.Attr("data-sidebar-item-icon", ""),
			glyph(item.Icon),
		))
	}
	if item.Prefix != "" && !(p.Collapsed && flavor == "admin") {
		content = append(content, h.Span(
			h.Class(prefixClass.Compile()),
			g.Attr("data-sidebar-item-prefix", "true"),
			g.Text(item.Prefix),
		))
	}
	content = append(content, h.Span(
		h.Class(labelClass.Compile()),
		g.Attr("data-sidebar-item-label", "true"),
		g.Text(item.Label),
	))
	if item.Badge != "" {
		content = append(content, Badge(BadgeProps{
			Label: item.Badge, Variant: item.BadgeVariant, Size: "sm",
		}))
	}
	if len(item.Children) > 0 {
		content = append(content, h.Span(
			g.Attr("data-sidebar-item-chevron", ""),
			glyph("chevron-down"),
		))
	}

	var itemLink g.Node
	if strings.TrimSpace(item.Href) != "" && !disabled {
		itemLink = h.A(append([]g.Node{h.Href(item.Href)}, content...)...)
	} else {
		itemLink = h.Span(content...)
	}
	if len(item.Children) == 0 {
		attrs = append(attrs, itemLink)
		return h.Li(attrs...)
	}

	children := make([]g.Node, 0, len(item.Children))
	for _, child := range item.Children {
		children = append(children, sidebarItemNode(p, child, flavor, depth+1))
	}
	attrs = append(attrs, h.Div(
		h.Class(clSidebarNestedGroup.Compile()),
		itemLink,
		h.Ul(
			h.Class(clSidebarNestedIndent.Compile()),
			g.Attr("data-sidebar-submenu", itemID),
			g.Group(children),
		),
	))
	return h.Li(attrs...)
}

func sidebarItemActive(item SidebarItem, current string) bool {
	if current == "" {
		if item.Active {
			return true
		}
	} else if current == item.Href ||
		(item.Href != "" && item.Href != "/admin" && strings.HasPrefix(current, item.Href)) {
		return true
	}
	for _, child := range item.Children {
		if sidebarItemActive(child, current) {
			return true
		}
	}
	return false
}

func sidebarResolvedID(explicit string, fallback string) string {
	if id := strings.TrimSpace(explicit); id != "" {
		return id
	}
	var normalized strings.Builder
	lastDash := false
	for _, char := range strings.ToLower(strings.TrimSpace(fallback)) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			normalized.WriteRune(char)
			lastDash = false
			continue
		}
		if !lastDash && normalized.Len() > 0 {
			normalized.WriteByte('-')
			lastDash = true
		}
	}
	id := strings.Trim(normalized.String(), "-")
	if id == "" {
		return "item"
	}
	return id
}

// Breadcrumb renders BreadcrumbProps as an aria-labelled trail; the
// current page is text, not a link, and carries aria-current.
func Breadcrumb(p BreadcrumbProps) g.Node {
	if len(p.Items) == 0 {
		return nil
	}
	sep := p.Separator
	if sep == "" {
		sep = "/"
	}
	visible := breadcrumbItems(p.Items, p.MaxItems)
	items := []g.Node{h.Class(clBreadcrumb.Compile())}
	for i, it := range visible {
		if i > 0 {
			items = append(items, h.Li(
				h.Class(clBreadcrumbSep.Compile()), g.Attr("aria-hidden", "true"), g.Text(sep),
			))
		}
		if it.Label == "…" && it.Href == "" && !it.Current {
			items = append(items, h.Li(
				h.Class(clBreadcrumbSep.Compile()),
				g.Attr("aria-hidden", "true"),
				g.Attr("data-breadcrumb-ellipsis", ""),
				g.Text("…"),
			))
			continue
		}
		current := it.Current || (it.Href == "" && i == len(visible)-1)
		if current {
			content := []g.Node{g.Text(it.Label)}
			if it.Icon != "" {
				content = append([]g.Node{glyph(it.Icon)}, content...)
			}
			items = append(items, h.Li(append(
				[]g.Node{h.Class(clBreadcrumbCur.Compile()), g.Attr("aria-current", "page")},
				content...,
			)...))
			continue
		}
		var adornment []g.Node
		if it.Icon != "" {
			adornment = []g.Node{glyph(it.Icon)}
		}
		items = append(items, h.Li(linkWithSlots(
			LinkProps{Label: it.Label, Href: it.Href},
			adornment,
		)))
	}
	nav := baseAttrs(p.ComponentProps)
	if p.Class != "" {
		nav = append(nav, h.Class(p.Class))
	}
	nav = append(nav, htmxAttrs(p.HTMXProps)...)
	nav = append(nav,
		g.Attr("data-component", "breadcrumb"),
		g.Attr("aria-label", "Breadcrumb"),
		h.Ol(items...),
	)
	return h.Nav(nav...)
}

func breadcrumbItems(items []BreadcrumbItem, maxItems int) []BreadcrumbItem {
	if maxItems <= 0 || len(items) <= maxItems || maxItems < 2 {
		return items
	}

	tailCount := max(maxItems-1, 1)
	startOfTail := len(items) - tailCount
	visible := make([]BreadcrumbItem, 0, maxItems+1)
	visible = append(visible, items[0], BreadcrumbItem{Label: "…"})
	return append(visible, items[startOfTail:]...)
}

// Pagination renders PaginationProps as previous/next plus a
// sibling window around the current page. Page links append ?page=N to
// BaseURL; when HTMX props are set they ride along on every link.
func Pagination(p PaginationProps) g.Node {
	if p.TotalPages <= 1 {
		return g.Text("")
	}
	siblings := p.Siblings
	if siblings <= 0 {
		siblings = 1
	}
	pageHref := func(n int) string {
		return paginationPageURL(p.BaseURL, n)
	}
	pageLink := func(n int, label string, current bool, ariaLabel, marker string) g.Node {
		cl := clPageBtn.Merge(clPageIdle)
		if current {
			cl = clPageBtn.Merge(clPageCur)
		}
		nodes := []g.Node{
			h.Class(cl.Compile()),
			h.Href(pageHref(n)),
			g.Attr("data-page", itoa(n)),
		}
		if marker != "" {
			nodes = append(nodes, g.Attr(marker, ""))
		}
		if current {
			nodes = append(nodes, g.Attr("aria-current", "page"))
		}
		if ariaLabel != "" {
			nodes = append(nodes, g.Attr("aria-label", ariaLabel))
		}
		if !current {
			enhancement := p.HTMXProps
			if hasHTMXEnhancement(enhancement) {
				enhancement.Get = pageHref(n)
			}
			nodes = append(nodes, htmxAttrs(enhancement)...)
		}
		return h.A(append(nodes, g.Text(label))...)
	}
	disabledBoundary := func(label, marker, glyph string) g.Node {
		return h.Button(
			h.Class(clPageBtn.Merge(clPageIdle).Compile()),
			h.Type("button"),
			g.Attr(marker, ""),
			g.Attr("aria-label", label),
			h.Disabled(),
			g.Text(glyph),
		)
	}

	items := []g.Node{h.Class(clPagination.Compile())}
	if p.CurrentPage > 1 {
		items = append(items, pageLink(p.CurrentPage-1, "‹", false, "Previous page", "data-pagination-prev"))
	} else {
		items = append(items, disabledBoundary("Previous page", "data-pagination-prev", "‹"))
	}
	lo, hi := p.CurrentPage-siblings, p.CurrentPage+siblings
	if lo < 1 {
		lo = 1
	}
	if hi > p.TotalPages {
		hi = p.TotalPages
	}
	if lo > 1 {
		items = append(items, pageLink(1, "1", p.CurrentPage == 1, "Go to page 1", ""))
		if lo > 2 {
			items = append(items, h.Span(h.Class(clBreadcrumbSep.Compile()), g.Text("…")))
		}
	}
	for n := lo; n <= hi; n++ {
		ariaLabel := "Go to page " + itoa(n)
		if n == p.CurrentPage {
			ariaLabel = "Page " + itoa(n) + ", current page"
		}
		items = append(items, pageLink(n, itoa(n), n == p.CurrentPage, ariaLabel, ""))
	}
	if hi < p.TotalPages {
		if hi < p.TotalPages-1 {
			items = append(items, h.Span(h.Class(clBreadcrumbSep.Compile()), g.Text("…")))
		}
		items = append(items, pageLink(p.TotalPages, itoa(p.TotalPages), false, "Go to page "+itoa(p.TotalPages), ""))
	}
	if p.CurrentPage < p.TotalPages {
		items = append(items, pageLink(p.CurrentPage+1, "›", false, "Next page", "data-pagination-next"))
	} else {
		items = append(items, disabledBoundary("Next page", "data-pagination-next", "›"))
	}

	nav := baseAttrs(p.ComponentProps)
	nav = append(nav, g.Attr("data-component", "pagination"), g.Attr("aria-label", "Pagination"))
	nav = append(nav, items...)
	return h.Nav(nav...)
}

func paginationPageURL(baseURL string, page int) string {
	parsed, err := url.Parse(baseURL)
	if err == nil {
		query := parsed.Query()
		query.Set("page", itoa(page))
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}

	separator := "?"
	if strings.Contains(baseURL, "?") {
		separator = "&"
	}
	return baseURL + separator + "page=" + itoa(page)
}

func hasHTMXEnhancement(p HTMXProps) bool {
	return p.Get != "" || p.Post != "" || p.Put != "" || p.Patch != "" ||
		p.Delete != "" || p.Target != "" || p.Swap != "" || p.Trigger != "" ||
		p.Confirm != "" || p.Ext != "" || p.Indicator != "" ||
		p.DisabledElt != "" || p.Vals != "" || p.PushURL != "" ||
		p.Select != "" || p.Boost || p.Disable
}

// TabSlot is one trusted Go tab-panel composition. Portable navigation-only
// tabs remain available through TabsProps.Items; rich panel bodies use this
// slot rather than a second component implementation.
type TabSlot struct {
	ID       string
	Label    string
	Icon     string
	Badge    string
	Disabled bool
	HxGet    string
	Content  []g.Node
}

// TabsSlots carries the ordered tab panels projected into TabsWithSlots.
type TabsSlots struct {
	Tabs []TabSlot
}

type tabsStyle struct {
	root, list, button, active, inactive string
}

// Tabs renders portable navigation tabs. Use TabsWithPanels when each tab owns
// a panel body on the current page.
func Tabs(p TabsProps) g.Node {
	style := resolveTabsStyle(p)
	active := activeItemKey(p.Items, p.ActiveTab)
	if p.Disabled {
		active = ""
	}
	items := []g.Node{
		h.Class(style.list),
		h.Role("tablist"),
		g.Attr("aria-orientation", tabsOrientation(p.Orientation)),
	}
	for _, it := range p.Items {
		disabled := it.Disabled || p.Disabled
		isActive := it.Key == active && !disabled
		stateClass := style.inactive
		if isActive {
			stateClass = style.active
		}
		className := strings.TrimSpace(style.button + " " + stateClass)
		if disabled {
			className += " " + clTabsDisabled.Compile()
		}
		node := []g.Node{
			h.Class(className),
			h.Role("tab"),
			g.Attr("aria-selected", boolText(isActive)),
			g.Attr("aria-disabled", boolText(disabled)),
		}
		if it.Icon != "" {
			node = append(node, h.Span(h.Class(clTabsIcon.Compile()), glyph(it.Icon)))
		}
		node = append(node, g.Text(it.Label))
		if it.Badge != "" {
			node = append(node, h.Span(h.Class(clTabsBadge.Compile()), g.Text(it.Badge)))
		}
		if it.URL != "" && !disabled {
			items = append(items, h.A(append(node, h.Href(it.URL))...))
			continue
		}
		button := append(node, h.Type("button"))
		if disabled {
			button = append(button, h.Disabled())
		}
		items = append(items, h.Button(button...))
	}
	rootProps := p.ComponentProps
	rootProps.Disabled = false
	nav := baseAttrs(rootProps)
	nav = append(nav, classes(style.root, p.Class), g.Attr("data-component", "tabs"))
	nav = append(nav, items...)
	return h.Nav(nav...)
}

// TabsWithPanels is the concise application API for controller-backed tabs.
func TabsWithPanels(p TabsProps, tabs ...TabSlot) g.Node {
	return TabsWithSlots(p, TabsSlots{Tabs: tabs})
}

// TabsWithSlots renders tabs and their panels from one canonical contract.
func TabsWithSlots(p TabsProps, slots TabsSlots) g.Node {
	if len(slots.Tabs) == 0 {
		return nil
	}

	style := resolveTabsStyle(p)
	active := activeSlotID(slots.Tabs, p.ActiveTab)
	if p.Disabled {
		active = ""
	}
	rootID := p.ID
	if rootID == "" {
		rootID = "tabs"
		if active != "" {
			rootID += "-" + active
		}
	}

	tabList := []g.Node{
		h.Class(style.list),
		h.Role("tablist"),
		g.Attr("aria-orientation", tabsOrientation(p.Orientation)),
	}
	panels := []g.Node{h.Class(clTabsPanels.Compile())}
	for _, tab := range slots.Tabs {
		disabled := tab.Disabled || p.Disabled
		isActive := tab.ID == active && !disabled
		panelID := rootID + "-panel-" + tab.ID
		tabID := rootID + "-tab-" + tab.ID
		stateClass := style.inactive
		if isActive {
			stateClass = style.active
		}
		buttonClass := strings.TrimSpace(style.button + " " + stateClass)
		if disabled {
			buttonClass += " " + clTabsDisabled.Compile()
		}
		button := []g.Node{
			h.Class(buttonClass), h.Type("button"), h.Role("tab"), h.ID(tabID),
			g.Attr("aria-controls", panelID),
			g.Attr("aria-selected", boolText(isActive)),
			g.Attr("aria-disabled", boolText(disabled)),
			g.Attr("tabindex", activeTabIndex(isActive)),
			g.Attr("data-tabs-tab", tab.ID),
			g.Attr("data-tabs-active-classes", style.active),
			g.Attr("data-tabs-inactive-classes", style.inactive),
			g.Attr("data-action", "click->tabs#activate"),
		}
		if disabled {
			button = append(button, h.Disabled())
		}
		if tab.Icon != "" {
			button = append(button, h.Span(h.Class(clTabsIcon.Compile()), glyph(tab.Icon)))
		}
		button = append(button, h.Span(g.Text(tab.Label)))
		if tab.Badge != "" {
			button = append(button, h.Span(h.Class(clTabsBadge.Compile()), g.Text(tab.Badge)))
		}
		tabList = append(tabList, h.Button(button...))

		panel := []g.Node{
			h.Class(clTabsPanel.Compile()), h.ID(panelID), h.Role("tabpanel"),
			g.Attr("aria-labelledby", tabID),
			g.Attr("aria-hidden", boolText(!isActive)),
			g.Attr("data-tabs-panel", tab.ID),
			g.Attr("data-state", tabState(isActive)),
		}
		if !isActive {
			panel = append(panel, g.Attr("hidden"))
		}
		hxGet := tab.HxGet
		if hxGet == "" {
			hxGet = p.HxGet
		}
		if hxGet != "" {
			panel = append(panel,
				g.Attr("data-tabs-lazy", "true"),
				g.Attr("hx-get", hxGet),
				g.Attr("hx-trigger", "tabs:activate from:this once"),
				g.Attr("hx-swap", "innerHTML"),
				h.Div(h.Class(clTabsLazy.Compile()),
					h.Span(h.Class(clTabsLazyLabel.Compile()), g.Text(fallbackText(p.LoadingLabel, "Loading...")))),
			)
		} else {
			panel = append(panel, tab.Content...)
		}
		panels = append(panels, h.Div(panel...))
	}

	rootProps := p.ComponentProps
	rootProps.Disabled = false
	root := baseAttrs(rootProps)
	root = append(root,
		classes(style.root, p.Class),
		g.Attr("data-component", "tabs"),
		g.Attr("data-controller", "tabs"),
		g.Attr("data-tabs-contract", "1"),
		g.Attr("data-tabs-active-tab-value", active),
		h.Div(tabList...),
		h.Div(panels...),
	)
	return h.Div(root...)
}

func resolveTabsStyle(p TabsProps) tabsStyle {
	orientation := tabsOrientation(p.Orientation)
	root := clTabsRoot.Merge(clTabsRootHorizontal)
	list := clTabsListBase.Merge(clTabsListHorizontal)
	button := clTabsButtonBase
	if orientation == "vertical" {
		root = clTabsRoot.Merge(clTabsRootVertical)
		list = clTabsListBase.Merge(clTabsListVertical)
	}
	if p.Variant == "pills" {
		button = button.Merge(clTabsButtonPills)
		return tabsStyle{root.Compile(), list.Compile(), button.Compile(), clTabsPillsActive.Compile(), clTabsPillsIdle.Compile()}
	}
	if orientation == "vertical" {
		list = list.Merge(clTabsListUnderlineVertical)
		button = button.Merge(clTabsButtonUnderlineVertical)
	} else {
		list = list.Merge(clTabsListUnderlineHorizontal)
		button = button.Merge(clTabsButtonUnderlineHorizontal)
	}
	return tabsStyle{root.Compile(), list.Compile(), button.Compile(), clTabsUnderlineActive.Compile(), clTabsUnderlineIdle.Compile()}
}

func tabsOrientation(value string) string {
	if value == "vertical" {
		return value
	}
	return "horizontal"
}

func activeItemKey(items []TabItem, requested string) string {
	for _, item := range items {
		if item.Key == requested && !item.Disabled {
			return item.Key
		}
	}
	for _, item := range items {
		if !item.Disabled {
			return item.Key
		}
	}
	return ""
}

func activeSlotID(tabs []TabSlot, requested string) string {
	for _, tab := range tabs {
		if tab.ID == requested && !tab.Disabled {
			return tab.ID
		}
	}
	for _, tab := range tabs {
		if !tab.Disabled {
			return tab.ID
		}
	}
	return ""
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func activeTabIndex(active bool) string {
	if active {
		return "0"
	}
	return "-1"
}

func tabState(active bool) string {
	if active {
		return "active"
	}
	return "inactive"
}
