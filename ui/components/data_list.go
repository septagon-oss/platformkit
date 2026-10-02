package components

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// DataListSlots keeps field projection and native forms with the caller.
type DataListSlots struct {
	TableSlots
	Toolbar, BulkActions, EmptyAction, RetryAction []g.Node
	// Footer holds collection commands before the pager. These are not selected-row writes.
	Footer     []g.Node
	RowActions func(DataRow) g.Node
}

// Validate refuses malformed composition before any row, token or URL is rendered.
func (p DataListProps) Validate() error {
	if strings.TrimSpace(p.Label) == "" {
		return fmt.Errorf("DataList: Label is required")
	}
	if err := p.State.Validate(); err != nil {
		return err
	}
	if !p.State.ready() && (len(p.Groups) != 0 || len(p.SelectedIDs) != 0) {
		return fmt.Errorf("DataList: absent content must not retain groups or selection")
	}
	// A failed read returns no result set either, so it keeps no part of an
	// earlier one: the same fields that describe a result are C1 correctable
	// before a byte or a capture leaves. Only refused content also loses Columns.
	if (p.State.Status == MediaFailed || p.State.Status == MediaRefused) && dataListRetainsResult(p) {
		return fmt.Errorf("DataList: absent content must clear result data and controls")
	}
	if p.State.Status == MediaRefused && len(p.Columns) != 0 {
		return fmt.Errorf("DataList: refused content must clear columns")
	}
	for _, choices := range [][]ChoiceLink{p.Filters, p.SortChoices, p.Views} {
		if err := validateChoices(choices); err != nil {
			return err
		}
	}
	if pager := p.Pagination; pager != nil && pager.TotalPages > 1 {
		if pager.CurrentPage < 1 || (pager.CurrentPage > pager.TotalPages && p.State.Status != MediaEmpty) || strings.TrimSpace(pager.BaseURL) == "" {
			return fmt.Errorf("DataList: invalid pagination range or URL")
		}
		for _, label := range []string{pager.NavigationLabel, pager.PreviousLabel, pager.NextLabel, pager.PageLabel, pager.CurrentPageLabel} {
			if strings.TrimSpace(label) == "" {
				return fmt.Errorf("DataList: pagination labels must be localized by the caller")
			}
		}
	}
	if p.LoadingLayout != nil && p.LoadingLayout.Rows < 1 {
		return fmt.Errorf("DataList: loading rows must be positive")
	}
	if !p.State.ready() {
		return nil
	}
	if len(p.Groups) == 0 || len(p.Columns) == 0 {
		return fmt.Errorf("DataList: ready requires groups and columns; use empty for no results")
	}
	columns, groups, rows := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, col := range p.Columns {
		if strings.TrimSpace(col.Key) == "" || columns[col.Key] || strings.TrimSpace(col.Label) == "" {
			return fmt.Errorf("DataList: columns require unique keys and labels")
		}
		columns[col.Key] = true
	}
	eligible := 0
	for _, group := range p.Groups {
		if strings.TrimSpace(group.Key) == "" || groups[group.Key] {
			return fmt.Errorf("DataList: group keys must be nonempty and unique")
		}
		groups[group.Key] = true
		if (len(p.Groups) > 1 || group.Collapsible) && (strings.TrimSpace(group.Title) == "" || strings.TrimSpace(group.CountText) == "") {
			return fmt.Errorf("DataList: named groups require Title and CountText")
		}
		if len(group.Rows) == 0 && strings.TrimSpace(group.CountText) == "" {
			return fmt.Errorf("DataList: an empty group requires CountText")
		}
		if group.Collapsed && !group.Collapsible {
			return fmt.Errorf("DataList: only collapsible groups may be collapsed")
		}
		for _, row := range group.Rows {
			if strings.TrimSpace(row.ID) == "" || rows[row.ID] {
				return fmt.Errorf("DataList: row IDs must be nonempty and unique")
			}
			rows[row.ID] = true
			if p.SelectionName != "" && strings.TrimSpace(row.SelectionLabel) == "" {
				return fmt.Errorf("DataList: every selection control requires SelectionLabel")
			}
			if row.Selectable {
				eligible++
			}
		}
	}
	if p.SelectionName != "" {
		for _, label := range []string{p.ID, p.FormID, p.ResultKey, p.SelectAllLabel, p.ClearSelectionLabel} {
			if strings.TrimSpace(label) == "" {
				return fmt.Errorf("DataList: selection requires ID, FormID, ResultKey and control labels")
			}
		}
		if len(p.SelectionCountLabels) != eligible+1 {
			return fmt.Errorf("DataList: SelectionCountLabels must cover zero through eligible count")
		}
		for _, label := range p.SelectionCountLabels {
			if strings.TrimSpace(label) == "" {
				return fmt.Errorf("DataList: selection count labels must be nonempty")
			}
		}
	} else if len(p.SelectedIDs) != 0 {
		return fmt.Errorf("DataList: SelectedIDs requires selection controls")
	}
	return nil
}

func DataList(p DataListProps) g.Node { return DataListWithSlots(p, DataListSlots{}) }

// DataListWithSlots keeps one Table owner per group and the server's row order.
func DataListWithSlots(p DataListProps, slots DataListSlots) g.Node {
	if err := p.Validate(); err != nil {
		return g.NodeFunc(func(io.Writer) error { return err })
	}
	if (p.State.Status == MediaFailed || p.State.Status == MediaRefused) && dataListRetainsSlots(p.State.Status, slots) {
		return invalidComponent(fmt.Errorf("DataList: absent content must clear slots"))
	}
	nodes := append(baseAttrs(p.ComponentProps), classes(clDataList.Compile(), p.Class),
		g.Attr("data-component", "data-list"), g.Attr("aria-label", p.Label), g.Attr("data-result-key", p.ResultKey))
	if p.State.Status == MediaLoading && p.LoadingLayout != nil {
		return dataListLoading(p)
	}
	nodes = append(nodes, slots.Toolbar...)
	nodes = append(nodes, choiceLinks(p.Views, p.Disabled), choiceLinks(p.Filters, p.Disabled), choiceLinks(p.SortChoices, p.Disabled))
	if p.ResultCountText != "" && (p.State.ready() || p.State.Status == MediaEmpty) {
		nodes = append(nodes, h.P(h.Class(clDataCount.Compile()), g.Text(p.ResultCountText)))
	}
	state := contentStateNode(p.State, slots.EmptyAction, slots.RetryAction,
		TableSkeleton(TableSkeletonProps{Columns: len(p.Columns), Compact: p.Compact}))
	if p.State.Status == MediaEmpty && len(p.Columns) != 0 {
		// An empty successful read keeps its schema and native sort navigation.
		// No row projection or selection callback is invoked.
		emptySlots := slots.TableSlots
		emptySlots.Empty = []g.Node{g.Text("")}
		state = g.Group{TableWithSlots(TableProps{HTMXProps: p.HTMXProps, Columns: p.Columns,
			Label: p.Label, Compact: p.Compact, Sortable: slots.SortURL != nil}, emptySlots),
			h.P(h.Class(clDataCount.Compile()), g.Text(p.State.Text)), g.Group(slots.EmptyAction)}
	}
	nodes = append(nodes, state)
	if !p.State.ready() {
		if p.State.Status == MediaEmpty {
			nodes = append(nodes, slots.Footer...)
			if p.Pagination != nil {
				nodes = append(nodes, Pagination(*p.Pagination))
			}
		}
		return h.Section(nodes...)
	}
	selected := map[string]bool{}
	for _, group := range p.Groups {
		for _, row := range group.Rows {
			if row.Selectable && !p.Disabled && slices.Contains(p.SelectedIDs, row.ID) {
				selected[row.ID] = true
			}
		}
	}
	for _, group := range p.Groups {
		rows := make([]TableRow, 0, len(group.Rows))
		byID := make(map[string]DataRow, len(group.Rows))
		for _, row := range group.Rows {
			rows, byID[row.ID] = append(rows, row.TableRow), row
		}
		tableSlots := slots.TableSlots
		tableSlots.SelectionName, tableSlots.SelectionFormID = p.SelectionName, p.FormID
		tableSlots.SelectAllLabel = p.SelectAllLabel
		tableSlots.SelectRowLabel = func(row TableRow) string { return byID[row.ID].SelectionLabel }
		tableSlots.SelectRowChecked = func(row TableRow) bool { return selected[row.ID] }
		tableSlots.SelectRowDisabled = func(row TableRow) bool { return p.Disabled || !byID[row.ID].Selectable }
		tableSlots.Cell = func(row TableRow, col TableColumn) g.Node {
			var cell g.Node
			if slots.Cell != nil {
				cell = slots.Cell(row, col)
			}
			if cell == nil && col.Primary && byID[row.ID].Href != "" {
				cell = recoveryAction(ButtonProps{Label: fmt.Sprint(row.Cells[col.Key]), Href: byID[row.ID].Href, Variant: "link"}, p.Disabled)
			}
			if !col.Primary {
				return cell
			}
			if cell == nil {
				cell = g.Text(fmt.Sprint(row.Cells[col.Key]))
			}
			content := []g.Node{cell}
			if data := byID[row.ID]; data.StatusLabel != "" {
				content = append(content, Badge(BadgeProps{Label: data.StatusLabel, Tone: data.Tone}))
			}
			if slots.RowActions != nil {
				content = append(content, slots.RowActions(byID[row.ID]))
			}
			return Flex(FlexProps{Gap: "2", Wrap: true, Align: "center"}, content...)
		}
		table := TableWithSlots(TableProps{HTMXProps: p.HTMXProps, Columns: p.Columns, Rows: rows,
			Label: cmp.Or(group.Title, p.Label), EmptyText: group.CountText, Selectable: p.SelectionName != "",
			Sortable: slots.SortURL != nil, Compact: p.Compact}, tableSlots)
		heading := []g.Node{g.Text(group.Title), h.Span(h.Class(clDataCount.Compile()), g.Text(group.CountText))}
		if group.Collapsible {
			nodes = append(nodes, h.Details(g.If(!group.Collapsed, h.Open()), h.Summary(append([]g.Node{h.Class(clDataDisclosure.Compile())}, heading...)...), table))
		} else {
			var body []g.Node
			if group.Title != "" {
				body = append(body, h.H2(append([]g.Node{h.Class(clDataHeading.Compile())}, heading...)...))
			}
			nodes = append(nodes, h.Div(append(body, table)...))
		}
	}
	if p.SelectionName != "" {
		labels := make([]g.Node, 0, len(p.SelectionCountLabels))
		for _, label := range p.SelectionCountLabels {
			labels = append(labels, h.Span(g.Text(label)))
		}
		nodes = append(nodes, h.Div(h.Hidden(""), g.Attr("data-selection-labels", ""), g.Group(labels)),
			h.P(h.Class(clDataCount.Compile()), h.Role("status"), g.Attr("aria-live", "polite"), g.Attr("data-selection-count", ""), g.Text(p.SelectionCountLabels[len(selected)])),
			h.Div(h.Hidden(""), g.Attr("data-component", "selection-control"), g.Attr("data-selection-clear", ""),
				recoveryAction(ButtonProps{Label: p.ClearSelectionLabel, Variant: "secondary", ComponentProps: ComponentProps{Attrs: map[string]string{"data-clear-selection": ""}}}, p.Disabled)))
		nodes = append(nodes, slots.BulkActions...)
	}
	nodes = append(nodes, slots.Footer...)
	if p.Pagination != nil {
		nodes = append(nodes, Pagination(*p.Pagination))
	}
	return h.Section(nodes...)
}

// The fields that describe a result set rather than the request for it. A
// failed or refused read returned none, so retaining one is a stale payload:
// the earlier result's filter URL, result key, selection or pager would reach
// the HTML and the typed capture without any row behind them.
func dataListRetainsResult(p DataListProps) bool {
	return len(p.Filters) != 0 || len(p.SortChoices) != 0 || len(p.Views) != 0 ||
		p.ResultKey != "" || p.SelectionName != "" || p.FormID != "" || p.SelectAllLabel != "" ||
		p.ClearSelectionLabel != "" || len(p.SelectionCountLabels) != 0 || p.ResultCountText != "" ||
		p.Pagination != nil || p.LoadingLayout != nil || p.HTMXProps != (HTMXProps{})
}

// A refused result may be captured as typed input, so hidden slots must be
// cleared even when this renderer would not place them in the HTML. A failed
// read keeps only its own recovery control.
func dataListRetainsSlots(status MediaStatus, slots DataListSlots) bool {
	table := slots.TableSlots
	return len(slots.Toolbar) != 0 || len(slots.BulkActions) != 0 || len(slots.EmptyAction) != 0 ||
		(status != MediaFailed && len(slots.RetryAction) != 0) || len(slots.Footer) != 0 || slots.RowActions != nil ||
		len(table.Empty) != 0 || table.Cell != nil || table.CellAttrs != nil || table.RowAttrs != nil ||
		table.SortURL != nil || table.SortState != nil || table.SortButtonAttrs != nil ||
		table.SelectAllLabel != "" || table.SelectRowLabel != nil || table.SelectRowChecked != nil ||
		table.SelectionName != "" || table.SelectionFormID != "" || table.SelectRowDisabled != nil
}

// The caller names the expected public geometry; no previous rows or slots are
// retained. TableSkeleton uses the same target and cell sizes as the ready table.
func dataListLoading(p DataListProps) g.Node {
	nodes := append(baseAttrs(p.ComponentProps), classes(clDataList.Compile(), p.Class),
		g.Attr("data-component", "data-list"), h.Role("status"), g.Attr("aria-label", p.State.LoadingLabel), g.Attr("aria-busy", "true"))
	placeholder := func(class string) g.Node {
		return h.Div(h.Class(class), g.Attr("aria-hidden", "true"), Skeleton(SkeletonProps{Shape: "text", Size: "lg"}))
	}
	if p.ResultCountText != "" {
		nodes = append(nodes, placeholder(clDataCount.Compile()))
	}
	var group []g.Node
	if p.LoadingLayout.GroupHeading {
		group = append(group, placeholder(clDataHeading.Compile()))
	}
	columns := len(p.Columns)
	if p.SelectionName != "" {
		columns++
	}
	group = append(group, TableSkeleton(TableSkeletonProps{Columns: columns, Rows: p.LoadingLayout.Rows, Compact: p.Compact, Interactive: true}))
	nodes = append(nodes, h.Div(group...))
	if p.SelectionName != "" {
		nodes = append(nodes, placeholder(clDataCount.Compile()), placeholder(clRecoveryAction.Compile()))
	}
	if p.LoadingLayout.BulkAction {
		nodes = append(nodes, h.Div(g.Attr("aria-hidden", "true"), Button(ButtonProps{Label: "\u00a0", Size: "lg", ComponentProps: ComponentProps{Disabled: true}})))
	}
	return h.Section(nodes...)
}
