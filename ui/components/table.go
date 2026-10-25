package components

// table.go renders the table, its scroll wrapper and its column widths. A width
// is a length this file recognizes or it is not emitted at all.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/style"
)

// tableWidth is the grammar of TableColumn.Width: a whole number and one of
// three units. It is the one caller value that reached a style attribute, and a
// column width is the sort of prop that gets filled in from a config file — so
// it is matched against a shape rather than interpolated, and anything else is
// no width at all rather than a declaration a browser might read as something
// else entirely.
var tableWidth = regexp.MustCompile(`^[0-9]+(px|%|rem)$`)

func width(value string) string {
	if tableWidth.MatchString(value) {
		return value
	}
	return ""
}

// TableSlots is the trusted Go composition seam for rich web cells and
// server-driven sorting. Portable table data remains in TableProps; callers
// only opt into these callbacks when a cell needs real markup.
type TableSlots struct {
	Empty             []g.Node
	Cell              func(TableRow, TableColumn) g.Node
	CellAttrs         func(TableRow, TableColumn) []g.Node
	RowAttrs          func(TableRow) []g.Node
	SortURL           func(TableColumn) string
	SortState         func(TableColumn) string
	SortButtonAttrs   func(TableColumn) []g.Node
	SelectAllLabel    string
	SelectRowLabel    func(TableRow) string
	SelectRowChecked  func(TableRow) bool
	SelectionName     string
	SelectionFormID   string
	SelectRowDisabled func(TableRow) bool
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
	lead := slices.IndexFunc(p.Columns, func(c TableColumn) bool { return !c.Stacked })
	head := tableHead(p, slots)

	tdClass := clTableTd
	if p.Compact {
		tdClass = clTableTdC
	}
	var bodyRows []g.Node
	for i, r := range p.Rows {
		bodyRows = append(bodyRows, tableRow(p, slots, tdClass, lead, i, r))
	}
	if len(bodyRows) == 0 {
		bodyRows = append(bodyRows, tableEmptyRow(p, slots))
	}

	wrap := tableWrapperAttrs(p)
	wrap = append(wrap,
		h.Table(
			h.Class(clTable.Compile()),
			h.THead(head...),
			h.TBody(bodyRows...),
		),
	)
	return h.Div(wrap...)
}

func tableHead(p TableProps, slots TableSlots) []g.Node {
	head := []g.Node{h.Class(clTableHead.Compile())}
	var headCells []g.Node
	if p.Selectable {
		label := fallbackText(slots.SelectAllLabel, "Select all rows")
		headCells = append(headCells, h.Th(
			h.Class(clTableTh.Compile()), g.Attr("scope", "col"),
			h.Label(h.Class(clTableSelection.Compile()), g.Attr("data-component", "selection-control"), h.Hidden(""),
				h.Input(h.Class(clCheckbox.Compile()), h.Type("checkbox"),
					g.Attr("data-pk-select", "all"), g.Attr("aria-label", label))),
		))
	}
	for _, c := range p.Columns {
		if p.Sortable && c.Sortable {
			sorter, sortState := sortControl(p, c, slots)
			headCells = append(headCells, h.Th(
				h.Class(stacked(clTableThSort, c).Compile()), g.Attr("scope", "col"),
				g.Attr("aria-sort", sortState),
				g.If(width(c.Width) != "", g.Attr("style", "width:"+width(c.Width))),
				sorter,
			))
			continue
		}
		cell := []g.Node{h.Class(stacked(clTableTh, c).Compile()), g.Attr("scope", "col")}
		if w := width(c.Width); w != "" {
			cell = append(cell, g.Attr("style", "width:"+w))
		}
		headCells = append(headCells, h.Th(append(cell, g.Text(c.Label))...))
	}
	head = append(head, h.Tr(headCells...), stackedSorters(p, slots, len(headCells)))
	return head
}

func tableRow(p TableProps, slots TableSlots, tdClass style.ClassList, lead, i int, r TableRow) g.Node {
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
		cells = append(cells, tableSelectionCell(p, slots, tdClass, r))
	}
	for i, c := range p.Columns {
		cells = append(cells, tableBodyCell(p, slots, tdClass, lead, i, r, c))
	}
	return h.Tr(cells...)
}

func tableSelectionCell(p TableProps, slots TableSlots, tdClass style.ClassList, r TableRow) g.Node {
	label := "Select row"
	if slots.SelectRowLabel != nil {
		label = fallbackText(slots.SelectRowLabel(r), label)
	}
	input := []g.Node{h.Class(clCheckbox.Compile()), h.Type("checkbox"),
		g.Attr("data-pk-select", r.ID), g.Attr("data-pk-select-row", ""), g.Attr("aria-label", label), h.Value(r.ID)}
	if slots.SelectionName != "" {
		input = append(input, h.Name(slots.SelectionName))
	}
	if slots.SelectionFormID != "" {
		input = append(input, g.Attr("form", slots.SelectionFormID))
	}
	if p.Disabled || slots.SelectRowDisabled != nil && slots.SelectRowDisabled(r) {
		input = append(input, h.Disabled())
	}
	if slots.SelectRowChecked != nil && slots.SelectRowChecked(r) {
		input = append(input, h.Checked())
	}
	return h.Td(h.Class(tdClass.Compile()), h.Label(h.Class(clTableSelection.Compile()), h.Input(input...)))
}

func tableBodyCell(p TableProps, slots TableSlots, tdClass style.ClassList, lead, i int, r TableRow, c TableColumn) g.Node {
	cell := stacked(tdClass, c)
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
	content := cellContent(r, c, slots)
	if i == lead {
		content = g.Group{content, stack(r, p.Columns, slots)}
	}
	bodyCell := h.Td(append(td, content)...)
	if c.RowHeader {
		bodyCell = h.Th(append(td, g.Attr("scope", "row"), content)...)
	}
	return bodyCell
}

func tableEmptyRow(p TableProps, slots TableSlots) g.Node {
	empty := p.EmptyText
	if empty == "" {
		empty = "Nothing to show yet."
	}
	span := len(p.Columns)
	if p.Selectable {
		span++
	}
	content := g.Node(g.Text(empty))
	if len(slots.Empty) != 0 {
		content = g.Group(slots.Empty)
	}
	return h.Tr(h.Td(
		h.Class(clTableTd.Compile()), h.ColSpan(itoa(span)),
		h.Class(clHelp.Compile()), content,
	))
}

func tableWrapperAttrs(p TableProps) []g.Node {
	wrap := baseAttrs(p.ComponentProps, htmxAttrs(p.HTMXProps)...)
	wrap = append(wrap,
		classes(clTableWrap.Compile(), p.Class),
		g.Attr("data-component", "table"),
	)
	if label := strings.TrimSpace(p.Label); label != "" {
		// Preserve caller-owned region attributes; duplicate attributes would
		// leave the browser using only the first value.
		if _, named := p.Attrs["role"]; !named {
			wrap = append(wrap, h.Role("region"))
		}
		if _, focused := p.Attrs["tabindex"]; !focused {
			wrap = append(wrap, g.Attr("tabindex", "0"))
		}
		if _, labelled := p.Attrs["aria-label"]; !labelled {
			wrap = append(wrap, g.Attr("aria-label", label))
		}
	}
	return wrap
}

// sortControl is a sortable column's header control and the aria-sort state its
// th declares: "none" unless SortState answers ascending or descending.
func sortControl(p TableProps, c TableColumn, slots TableSlots) (g.Node, string) {
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
	control := []g.Node{
		h.Class(clTableSortBtn.Compile()),
		g.Attr("data-pk-sort", c.Key),
		g.Text(c.Label),
		h.Span(g.Attr("aria-hidden", "true"), g.Attr("data-pk-sort-icon", ""), g.Text(glyph)),
	}
	sortURL := ""
	if slots.SortURL != nil {
		sortURL = slots.SortURL(c)
	}
	if sortURL != "" {
		enhancement := p.HTMXProps
		enhancement.Get = sortURL
		enhancement.Trigger = ""
		control = append(control, htmxAttrs(enhancement)...)
	}
	if slots.SortButtonAttrs != nil {
		control = append(control, slots.SortButtonAttrs(c)...)
	}
	// A server-sorted column is a link, because that is what it is: a
	// different URL showing the same table in another order, which a
	// person can open in a tab, bookmark, or reach with no JavaScript
	// running at all. The hx-get above is an enhancement on top of the
	// href, not the thing that makes the header work. Where there is no
	// URL the sort is the page script's, and a button is right.
	if sortURL != "" {
		return h.A(append(control, h.Href(sortURL))...), sortState
	}
	return h.Button(append(control, h.Type("button"))...), sortState
}

// stackedSorters is the header row that lists the sort controls of the sortable
// stacked columns, for a phone below the sm breakpoint where those columns' own
// headers are not drawn; nothing when no stacked column sorts. A plain cell in
// a row of its own, so no column header's name carries another column's label.
func stackedSorters(p TableProps, slots TableSlots, span int) g.Node {
	var controls []g.Node
	for _, c := range p.Columns {
		if c.Stacked && p.Sortable && c.Sortable {
			sorter, _ := sortControl(p, c, slots)
			controls = append(controls, sorter)
		}
	}
	if len(controls) == 0 {
		return nil
	}
	return h.Tr(h.Class(clTableStackSortRow.Compile()),
		h.Td(g.Attr("colspan", fmt.Sprint(span)),
			h.Div(append([]g.Node{h.Class(clTableStackSort.Compile())}, controls...)...)))
}

// cellContent is what a column's cell shows for a row: the trusted slot's node
// when it answers one, the cell's own text otherwise.
func cellContent(r TableRow, c TableColumn, slots TableSlots) g.Node {
	if slots.Cell != nil {
		if rich := slots.Cell(r, c); rich != nil {
			return rich
		}
	}
	if raw, ok := r.Cells[c.Key]; ok && raw != nil {
		return g.Text(fmt.Sprint(raw))
	}
	return g.Text("")
}

// stack lists a row's stacked columns, label then value, for the row's first
// unstacked cell to carry below the sm breakpoint; nothing when no column is
// stacked. The value is the node the column's own cell shows, so the two
// cannot say different things.
func stack(r TableRow, columns []TableColumn, slots TableSlots) g.Node {
	var items []g.Node
	for _, c := range columns {
		if c.Stacked {
			items = append(items, h.Div(h.Class(clTableStackItem.Compile()),
				h.Dt(h.Class(clTableThBase.Compile()), g.Text(c.Label)),
				h.Dd(h.Class(clTableStackValue.Compile()), cellContent(r, c, slots))))
		}
	}
	if len(items) == 0 {
		return nil
	}
	return h.Dl(append([]g.Node{h.Class(clTableStack.Compile())}, items...)...)
}

// stacked adds the stacked column's breakpoint to a header or cell class.
func stacked(cl style.ClassList, c TableColumn) style.ClassList {
	if c.Stacked {
		return cl.Merge(clTableStacked)
	}
	return cl
}
