package components

// table.go renders the table, its scroll wrapper and its column widths. A width
// is a length this file recognizes or it is not emitted at all.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"fmt"
	"regexp"
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
			var sorter g.Node = h.Button(append(control, h.Type("button"))...)
			if sortURL != "" {
				sorter = h.A(append(control, h.Href(sortURL))...)
			}
			headCells = append(headCells, h.Th(
				h.Class(clTableThSort.Compile()), g.Attr("scope", "col"),
				g.Attr("aria-sort", sortState),
				g.If(width(c.Width) != "", g.Attr("style", "width:"+width(c.Width))),
				sorter,
			))
			continue
		}
		cell := []g.Node{h.Class(clTableTh.Compile()), g.Attr("scope", "col")}
		if w := width(c.Width); w != "" {
			cell = append(cell, g.Attr("style", "width:"+w))
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
	)
	if label := strings.TrimSpace(p.Label); label != "" {
		// A caller that declared one of these for itself is honoured, and is not
		// written over: two attributes of the same name are invalid HTML, and a
		// browser keeps the first, which is the caller's — so emitting ours as well
		// would only make the markup claim an attribute that does not apply.
		// What refuses a scroll box nobody can reach is e2e/design-audit.spec.ts,
		// which measures the rendered page rather than trusting this function.
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
	wrap = append(wrap,
		h.Table(
			h.Class(clTable.Compile()),
			h.THead(head...),
			h.TBody(bodyRows...),
		),
	)
	return h.Div(wrap...)
}
