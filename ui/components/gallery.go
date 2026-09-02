package components

// gallery.go is the package's account of itself: one representative instance of
// every component it renders, with the props that reach the branches worth
// seeing.
//
// It is production code and not a fixture, because it has two callers that must
// not drift. The admin shell serves it at /admin/_gallery, which is how a
// person sees what the design system looks like in the theme they are running.
// The tests render it to prove that every class a renderer can emit is declared
// in classlists.go and resolves to a rule — so a component that is not in this
// list is a component whose classes the closure test never sees.
//
// Adding a component means adding a line here. That is the ratchet.

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Example is one entry in the gallery: what it is called and what it renders.
type Example struct {
	// Group is the heading it appears under.
	Group string
	// Name is the component and the variant, "Button / primary".
	Name string
	Node g.Node
}

// Gallery is every component this package renders, once each per variant worth
// distinguishing.
func Gallery() []Example {
	no := false
	e := func(group, name string, node g.Node) Example { return Example{Group: group, Name: name, Node: node} }
	return []Example{
		e("Type", "Heading / 1", Heading(HeadingProps{Text: "Page title", Level: 1})),
		e("Type", "Heading / 2", Heading(HeadingProps{Text: "Section", Level: 2, Anchor: "section"})),
		e("Type", "Heading / 3", Heading(HeadingProps{Text: "Sub", Level: 3})),
		e("Type", "Heading / 4", Heading(HeadingProps{Text: "Minor", Level: 4})),
		e("Type", "Heading / 5", Heading(HeadingProps{Text: "Small", Level: 5})),
		e("Type", "Heading / 6", Heading(HeadingProps{Text: "Eyebrow", Level: 6})),
		e("Type", "Text / muted", Text(TextProps{Content: "Plain body copy.", Size: "sm", Color: "muted"})),
		e("Type", "Text / loud", Text(TextProps{Content: "Loud.", Size: "xl", Weight: "bold", Color: "brand"})),
		e("Type", "Text / truncated", Text(TextProps{Content: "Cut off eventually.", Truncate: true})),
		e("Type", "Link / internal", Link(LinkProps{Label: "Internal", Href: "/docs"})),
		e("Type", "Link / external", Link(LinkProps{Label: "External", Href: "https://example.test", External: true})),
		e("Type", "Divider", Divider(DividerProps{})),
		e("Type", "Divider / labelled", Divider(DividerProps{Text: "Or continue with"})),
		e("Type", "Divider / vertical", Divider(DividerProps{Orientation: "vertical"})),

		e("Icon", "Icon / check", Icon(IconProps{Name: "check", Size: "lg", Tone: "success"})),
		e("Icon", "Icon / trash", Icon(IconProps{Name: "trash", Size: "md", Tone: "danger"})),
		e("Icon", "Icon / unknown", Icon(IconProps{Name: "no-such-glyph"})),

		e("Action", "Button / primary", Button(ButtonProps{Label: "Save", Variant: "primary", Tone: "neutral", Size: "md"})),
		e("Action", "Button / secondary", Button(ButtonProps{Label: "Cancel", Variant: "secondary", Tone: "neutral", Size: "sm"})),
		e("Action", "Button / danger", Button(ButtonProps{Label: "Delete", Variant: "primary", Tone: "danger", Size: "xs"})),
		e("Action", "Button / ghost", Button(ButtonProps{Label: "Ghost", Variant: "ghost", Tone: "neutral", Size: "lg"})),
		e("Action", "Button / link", Button(ButtonProps{Label: "Docs", Variant: "link", Tone: "neutral", Size: "xl"})),
		e("Action", "Button / outline full", Button(ButtonProps{Label: "Outline", Variant: "outline", Tone: "neutral", Size: "2xl", FullWidth: true})),
		e("Action", "Button / info", Button(ButtonProps{Label: "Info", Variant: "primary", Tone: "info"})),
		e("Action", "Button / warning", Button(ButtonProps{Label: "Warn", Variant: "primary", Tone: "warning"})),
		e("Action", "Button / success", Button(ButtonProps{Label: "OK", Variant: "primary", Tone: "success"})),
		e("Action", "Button / loading", Button(ButtonProps{Label: "Busy", Loading: true})),
		e("Action", "Button / as link", Button(ButtonProps{Label: "Open", Href: "/somewhere"})),
		e("Action", "Button / with icon", ButtonWithSlots(
			ButtonProps{Label: "Iconed", Variant: "primary", Tone: "neutral", Size: "md"},
			ButtonSlots{IconEnd: []g.Node{Icon(IconProps{Name: "plus", Size: "md", Tone: "neutral"})}})),

		e("Status", "Badge / default", Badge(BadgeProps{Label: "Default"})),
		e("Status", "Badge / brand dot", Badge(BadgeProps{Label: "New", Variant: "primary", Tone: "brand", Dot: true})),
		e("Status", "Badge / success", Badge(BadgeProps{Label: "OK", Tone: "success"})),
		e("Status", "Badge / warning", BadgeWithSlots(BadgeProps{Label: "Careful", Tone: "warning"},
			BadgeSlots{IconStart: []g.Node{Icon(IconProps{Name: "warning", Size: "sm", Tone: "warning"})}})),
		e("Status", "Badge / danger", Badge(BadgeProps{Label: "Bad", Tone: "danger"})),
		e("Status", "Badge / info", Badge(BadgeProps{Label: "FYI", Tone: "info"})),
		e("Status", "Badge / secondary", Badge(BadgeProps{Label: "Two", Variant: "secondary"})),
		e("Status", "Badge / outline", Badge(BadgeProps{Label: "Outlined", Variant: "outline"})),
		e("Status", "Badge / count", Badge(BadgeProps{Label: "Messages", Count: 120, Removable: true, Live: true})),

		e("Status", "Alert / success", Alert(AlertProps{Title: "Saved", Message: "All good.", Tone: "success"})),
		e("Status", "Alert / warning", alertWithSlots(AlertProps{Message: "Careful now.", Tone: "warning"},
			[]g.Node{Icon(IconProps{Name: "warning", Size: "md", Tone: "warning"})}, nil)),
		e("Status", "Alert / danger", Alert(AlertProps{Message: "That failed.", Tone: "danger"})),
		e("Status", "Alert / info", Alert(AlertProps{Message: "Heads up.", Tone: "info"})),
		e("Status", "Alert / compact", Alert(AlertProps{Message: "Compact.", Tone: "info", Compact: true})),
		e("Status", "Alert / bordered", Alert(AlertProps{Message: "Accented.", Tone: "warning", Bordered: true})),
		e("Status", "Alert / dismissible", Alert(AlertProps{Message: "Dismiss me.", Tone: "success", Dismissible: true})),

		e("Status", "Spinner / brand", Spinner(SpinnerProps{Size: "sm", Tone: "brand"})),
		e("Status", "Spinner / labelled", Spinner(SpinnerProps{Size: "md", Tone: "info", Label: "Fetching"})),
		e("Status", "Spinner / success", Spinner(SpinnerProps{Size: "lg", Tone: "success"})),

		e("Status", "Skeleton / block", Skeleton(SkeletonProps{})),
		e("Status", "Skeleton / block sm", Skeleton(SkeletonProps{Shape: "block", Size: "sm"})),
		e("Status", "Skeleton / block lg", Skeleton(SkeletonProps{Shape: "block", Size: "lg"})),
		e("Status", "Skeleton / text", Skeleton(SkeletonProps{Shape: "text", Lines: 3, Size: "sm"})),
		e("Status", "Skeleton / circle", Skeleton(SkeletonProps{Shape: "circle"})),
		e("Status", "Skeleton / table", TableSkeleton(TableSkeletonProps{})),
		e("Status", "Skeleton / table compact", TableSkeleton(TableSkeletonProps{Columns: 2, Rows: 5, Compact: true})),

		e("Status", "EmptyState", emptyStateWithSlots(
			EmptyStateProps{Title: "No tenants yet", Description: "Create the first tenant to get started.", Bordered: true},
			nil, []g.Node{Link(LinkProps{Label: "New tenant", Href: "/admin/tenants/new"})})),
		e("Status", "EmptyState / compact", EmptyState(EmptyStateProps{Title: "Empty", Compact: true})),

		e("Form", "Label", Label(LabelProps{Text: "Standalone", For: "x", Required: true})),
		e("Form", "Input / email", Input(InputProps{Name: "email", Type: "email", Label: "Email",
			Placeholder: "you@example.test", HelpText: "We never share it.", Required: true})),
		e("Form", "Input / invalid", Input(InputProps{Name: "slug", Label: "Slug", Value: "hello",
			Error: "Already taken.", Pattern: "[a-z-]+"})),
		e("Form", "Input / bare", Input(InputProps{Name: "quiet"})),
		e("Form", "Input / read-only", Input(InputProps{Name: "id", Label: "Id", Value: "42", ReadOnly: true})),
		e("Form", "Select", Select(SelectProps{Name: "kind", Label: "Kind", Required: true,
			Options: []SelectOption{{Label: "Post", Value: "post"}, {Label: "Page", Value: "page"}},
			Value:   "post", HelpText: "What the entry renders as."})),
		e("Form", "Select / invalid", Select(SelectProps{Name: "state", Label: "State", Placeholder: "Any state",
			Options: []SelectOption{{Label: "Draft", Value: "draft"}}, Error: "Pick a state."})),
		e("Form", "Textarea", Textarea(TextareaProps{Name: "body", Label: "Body", Rows: 6,
			HelperText: "Markdown is fine.", MaxLength: 500})),
		e("Form", "Textarea / invalid", Textarea(TextareaProps{Name: "bad", Label: "Bad", ErrorMessage: "Too long."})),
		e("Form", "Textarea / autoresize", Textarea(TextareaProps{
			ComponentProps: ComponentProps{Disabled: true},
			Name:           "details", Label: "Details", Value: "Existing", HelperText: "Add context.",
			ErrorMessage: "More detail is required.", AutoResize: true, MinRows: 3, MaxRows: 15, FullWidth: true})),
		e("Form", "Checkbox", Checkbox(CheckboxProps{Name: "agree", Label: "I agree", Required: true})),
		e("Form", "Checkbox / checked", Checkbox(CheckboxProps{Name: "done", Label: "Done", Checked: true})),
		e("Form", "Checkbox / indeterminate", Checkbox(CheckboxProps{Name: "some", Label: "Some", Indeterminate: true})),
		e("Form", "Checkbox / disabled", Checkbox(CheckboxProps{
			ComponentProps: ComponentProps{Disabled: true}, Name: "disabled", Label: "Disabled"})),

		e("Layout", "Stack", Stack(StackProps{Gap: "2", Align: "start"}, g.Text("a"), g.Text("b"))),
		e("Layout", "Flex", Flex(FlexProps{Direction: "row", Gap: "4", Align: "center",
			Justify: "between", Wrap: true}, g.Text("l"), g.Text("r"))),
		e("Layout", "Grid", Grid(GridProps{Columns: "3", Gap: "6"}, g.Text("1"), g.Text("2"), g.Text("3"))),
		e("Layout", "Container", Container(ContainerProps{MaxWidth: "4xl"}, g.Text("content"))),
		e("Layout", "Card", Card(CardProps{Title: "Plain card", Description: "With copy."})),
		e("Layout", "Card / clickable", Card(CardProps{Title: "Go somewhere", Clickable: true, Href: "/detail"})),

		e("Data", "Table", Table(TableProps{
			Columns: []TableColumn{{Key: "name", Label: "Name"}, {Key: "role", Label: "Role"}},
			Rows: []TableRow{
				{ID: "u1", Cells: map[string]any{"name": "Ada", "role": "admin"}},
				{ID: "u2", Cells: map[string]any{"name": "Lin", "role": 7}},
			}})),
		e("Data", "Table / empty", Table(TableProps{
			Columns: []TableColumn{{Key: "a", Label: "A"}}, EmptyText: "No rows.", Compact: true})),
		e("Data", "Table / sortable", Table(TableProps{
			Sortable: true, Striped: true, Selectable: true,
			Columns: []TableColumn{
				{Key: "name", Label: "Name", Sortable: true, Primary: true},
				{Key: "count", Label: "Count", Sortable: true, Align: "right"},
				{Key: "note", Label: "Note"},
			},
			Rows: []TableRow{
				{ID: "r1", Cells: map[string]any{"name": "First", "count": 3, "note": "odd"}},
				{ID: "r2", Cells: map[string]any{"name": "Second", "count": 1, "note": "even"}},
				{ID: "r3", Cells: map[string]any{"name": "Third", "count": 2}},
			}})),
		e("Data", "DetailList", DetailList(DetailListProps{
			ComponentProps: ComponentProps{ID: "account-facts"},
			Title:          "Profile",
			Description:    "Identity used across this workspace.",
			SemanticRole:   "identity",
			Items: []DetailItem{
				{Label: "Email", Value: "ada@example.test", Tone: "neutral"},
				{Label: "Plan", Value: "Studio", Description: "Renews next month.", Tone: "brand"},
				{Label: "Health", Value: "Good", Tone: "success"},
				{Label: "Review", Value: "Soon", Tone: "warning"},
				{Label: "Risk", Value: "High", Tone: "danger"},
				{Label: "Region", Value: "EU", Tone: "info"},
			}})),

		e("Navigation", "Breadcrumb", Breadcrumb(BreadcrumbProps{Items: []BreadcrumbItem{
			{Label: "Home", Href: "/"}, {Label: "Tenants", Href: "/tenants"}, {Label: "Acme"},
		}})),
		e("Navigation", "Pagination", Pagination(PaginationProps{CurrentPage: 5, TotalPages: 12, BaseURL: "/rows"})),
		e("Navigation", "Pagination / two pages", Pagination(PaginationProps{CurrentPage: 1, TotalPages: 2, BaseURL: "/few"})),
		e("Navigation", "Tabs", Tabs(TabsProps{ActiveTab: "b", Items: []TabItem{
			{Key: "a", Label: "First", URL: "/tab/a"}, {Key: "b", Label: "Second"},
		}})),
		e("Navigation", "Tabs / vertical pills", TabsWithPanels(
			TabsProps{ActiveTab: "profile", Orientation: "vertical", Variant: "pills"},
			TabSlot{ID: "profile", Label: "Profile", Icon: "user", Badge: "New", Content: []g.Node{g.Text("Profile panel")}},
			TabSlot{ID: "security", Label: "Security", Disabled: true, Content: []g.Node{g.Text("Security panel")}},
			TabSlot{ID: "activity", Label: "Activity", HxGet: "/activity"})),
		e("Navigation", "Sidebar", SidebarWithSlots(SidebarProps{
			Current: "/admin/customers/accounts",
			Sections: []SidebarSection{{
				ID: "operate", Label: "Operate", Glyph: "O", Tone: "brand",
				Items: []SidebarItem{
					{Label: "Dashboard", Href: "/admin", Icon: "gear"},
					{Label: "Customers", Href: "/admin/customers", Icon: "user", Badge: "24", Children: []SidebarItem{
						{Label: "Accounts", Href: "/admin/customers/accounts"},
					}},
				},
			}},
		}, SidebarSlots{Footer: []g.Node{g.Text("Signed in")}})),
		e("Navigation", "Sidebar / content flavor", SidebarWithSlots(SidebarProps{
			ComponentProps: ComponentProps{ID: "docs-sidebar"},
			Flavor:         "content", Current: "#two", NavigationLabel: "Documentation sections",
			Sections: []SidebarSection{{
				ID: "runtime", Label: "Runtime", Glyph: "R", Tone: "info", SearchText: "runtime docs",
				Items: []SidebarItem{
					{ID: "one", Label: "Overview", Href: "#one", Prefix: "01", SearchText: "overview"},
					{ID: "two", Label: "Handoff", Href: "#two", Prefix: "02", SearchText: "handoff"},
				},
			}},
		}, SidebarSlots{Brand: []g.Node{h.Strong(g.Text("Documentation"))}, Footer: []g.Node{g.Text("Version 1")}})),
		e("Navigation", "Sidebar / collapsed", Sidebar(SidebarProps{
			ComponentProps: ComponentProps{Disabled: true},
			Collapsible:    true, Collapsed: true,
			Items: []SidebarItem{
				{Label: "Home", Href: "/admin", Icon: "gear"},
				{Label: "Reports", Href: "/admin/reports", Icon: "file-text", Disabled: true},
			}})),

		e("Overlay", "Modal", Modal(ModalProps{ComponentProps: ComponentProps{ID: "confirm-modal"},
			Title: "Archive", Description: "This action cannot be undone.",
			Body: "Review the affected records.", Footer: "Confirm or cancel.", Size: "small", Open: true})),
		e("Overlay", "Modal / medium", Modal(ModalProps{Title: "Edit record", Size: "medium"})),
		e("Overlay", "Modal / large", Modal(ModalProps{Title: "Large review", Size: "large"})),
		e("Overlay", "Modal / xl", Modal(ModalProps{Title: "Wide review", Size: "xl"})),
		e("Overlay", "Modal / undismissable", Modal(ModalProps{AriaLabel: "Required decision", Size: "full",
			Closable: &no, CloseOnOverlay: &no, CloseOnEscape: &no, ShowClose: &no, ShowOverlay: &no, Centered: &no})),
		e("Overlay", "Modal / deferred", Modal(ModalProps{ComponentProps: ComponentProps{ID: "server-modal"},
			AriaLabel: "Server dialog", Deferred: true, OpenOnSwap: true})),
		e("Overlay", "Modal / panel", ModalPanel(ModalProps{Title: "Panel only"}, g.Text("Body"))),
		e("Overlay", "Modal / form", ModalForm(g.Text("Fields"))),
		e("Overlay", "Modal / close button", ModalCloseButton("Close", "")),
		e("Overlay", "Modal / cancel button", ModalCancelButton("Cancel", "")),
		e("Overlay", "ConfirmDialog", ConfirmDialog(ConfirmDialogProps{
			Title: "Delete this row?", AcceptLabel: "Delete", CancelLabel: "Keep"})),

		e("Frame", "SkipLink", SkipLink("content", "Skip to content")),
		e("Frame", "Toolbar", Toolbar(ToolbarProps{Title: "Tasks", Subtitle: "12 records"},
			Button(ButtonProps{Label: "New task", Href: "/admin/task/tasks/new"}))),
		e("Frame", "Form", Form(FormProps{Action: "/admin/task/tasks", Label: "New task"},
			Input(InputProps{Name: "title", Label: "Title", Required: true}),
			FormActions(FormActionsProps{},
				Button(ButtonProps{Label: "Cancel", Variant: "secondary", Href: "/admin/task/tasks"}),
				Button(ButtonProps{Label: "Create", Type: "submit"})))),
		e("Frame", "Shell", Shell(ShellProps{SkipTarget: "gallery-main"}, ShellSlots{
			Sidebar: []g.Node{Sidebar(SidebarProps{Items: []SidebarItem{{Label: "Tasks", Href: "/admin/task/tasks"}}})},
			Header:  []g.Node{Text(TextProps{Content: "Acme", Weight: "semibold"})},
			Main:    []g.Node{Text(TextProps{Content: "Page content"})},
			Footer:  []g.Node{Text(TextProps{Content: "PlatformKit", Size: "xs", Color: "muted"})},
		})),
	}
}
