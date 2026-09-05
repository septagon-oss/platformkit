package components

// gallery.go is the package's account of itself: one representative instance of
// every component it renders, with typed props and composition slots captured
// before rendering. Gallery, contract export and class-closure checks use the
// same examples; no second component list describes the design surface.
//
// It is production code and not a fixture: its callers must not drift. The
// admin shell serves it at /admin/_gallery, which is how a
// person sees what the design system looks like in the theme they are running.
// The tests render it to prove that every class a renderer can emit is declared
// in classlists.go and resolves to a rule — so a component that is not in this
// list is a component whose classes the closure test never sees.
//
// Adding a component means adding a line here. That is the ratchet.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Gallery is every component this package renders, once each per variant worth
// distinguishing.
func Gallery() []Example {
	// IDs are explicit and survive changes to labels, grouping and display order.
	info := func(id, group, name string) ExampleInfo {
		componentID, _, _ := strings.Cut(id, "/")
		return ExampleInfo{ID: id, ComponentID: componentID, Group: group, Name: name}
	}
	return []Example{
		ExampleOf(info("pk-ui.component.heading/1", "Type", "Heading / 1"), HeadingProps{Text: "Page title", Level: 1}, Heading),
		ExampleOf(info("pk-ui.component.heading/2", "Type", "Heading / 2"), HeadingProps{Text: "Section", Level: 2, Anchor: "section"}, Heading),
		ExampleOf(info("pk-ui.component.heading/3", "Type", "Heading / 3"), HeadingProps{Text: "Sub", Level: 3}, Heading),
		ExampleOf(info("pk-ui.component.heading/4", "Type", "Heading / 4"), HeadingProps{Text: "Minor", Level: 4}, Heading),
		ExampleOf(info("pk-ui.component.heading/5", "Type", "Heading / 5"), HeadingProps{Text: "Small", Level: 5}, Heading),
		ExampleOf(info("pk-ui.component.heading/6", "Type", "Heading / 6"), HeadingProps{Text: "Eyebrow", Level: 6}, Heading),
		ExampleOf(info("pk-ui.component.text/muted", "Type", "Text / muted"), TextProps{Content: "Plain body copy.", Size: "sm", Color: "muted"}, Text),
		ExampleOf(info("pk-ui.component.text/loud", "Type", "Text / loud"), TextProps{Content: "Loud.", Size: "xl", Weight: "bold", Color: "brand"}, Text),
		ExampleOf(info("pk-ui.component.text/truncated", "Type", "Text / truncated"), TextProps{Content: "Cut off eventually.", Truncate: true}, Text),
		ExampleOf(info("pk-ui.component.link/internal", "Type", "Link / internal"), LinkProps{Label: "Internal", Href: "/docs"}, Link),
		ExampleOf(info("pk-ui.component.link/external", "Type", "Link / external"), LinkProps{Label: "External", Href: "https://example.test", External: true}, Link),
		ExampleOf(info("pk-ui.component.divider/default", "Type", "Divider"), DividerProps{}, Divider),
		ExampleOf(info("pk-ui.component.divider/labelled", "Type", "Divider / labelled"), DividerProps{Text: "Or continue with"}, Divider),
		ExampleOf(info("pk-ui.component.divider/vertical", "Type", "Divider / vertical"), DividerProps{Orientation: "vertical"}, Divider),

		ExampleOf(info("pk-ui.component.icon/check", "Icon", "Icon / check"), IconProps{Name: "check", Size: "lg", Tone: "success"}, Icon),
		ExampleOf(info("pk-ui.component.icon/trash", "Icon", "Icon / trash"), IconProps{Name: "trash", Size: "md", Tone: "danger"}, Icon),
		ExampleOf(info("pk-ui.component.icon/unknown", "Icon", "Icon / unknown"), IconProps{Name: "no-such-glyph"}, Icon),

		ExampleWithSlots(info("pk-ui.component.button/primary", "Action", "Button / primary"),
			ButtonProps{Label: "Save", Variant: "primary", Tone: "neutral", Size: "md"}, ButtonSlots{}, ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/secondary", "Action", "Button / secondary"),
			ButtonProps{Label: "Cancel", Variant: "secondary", Tone: "neutral", Size: "sm"}, ButtonSlots{}, ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/danger", "Action", "Button / danger"),
			ButtonProps{Label: "Delete", Variant: "primary", Tone: "danger", Size: "xs"}, ButtonSlots{}, ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/ghost", "Action", "Button / ghost"),
			ButtonProps{Label: "Ghost", Variant: "ghost", Tone: "neutral", Size: "lg"}, ButtonSlots{}, ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/link", "Action", "Button / link"), ButtonProps{Label: "Docs", Variant: "link", Tone: "neutral", Size: "xl"}, ButtonSlots{}, ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/outline-full", "Action", "Button / outline full"),
			ButtonProps{Label: "Outline", Variant: "outline", Tone: "neutral", Size: "2xl", FullWidth: true}, ButtonSlots{}, ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/info", "Action", "Button / info"), ButtonProps{Label: "Info", Variant: "primary", Tone: "info"}, ButtonSlots{}, ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/warning", "Action", "Button / warning"), ButtonProps{Label: "Warn", Variant: "primary", Tone: "warning"}, ButtonSlots{}, ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/success", "Action", "Button / success"), ButtonProps{Label: "OK", Variant: "primary", Tone: "success"}, ButtonSlots{}, ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/loading", "Action", "Button / loading"), ButtonProps{Label: "Busy", Loading: true}, ButtonSlots{}, ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/as-link", "Action", "Button / as link"), ButtonProps{Label: "Open", Href: "/somewhere"}, ButtonSlots{}, ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/with-icon", "Action", "Button / with icon"),
			ButtonProps{Label: "Iconed", Variant: "primary", Tone: "neutral", Size: "md"}, ButtonSlots{IconEnd: []g.Node{Icon(IconProps{Name: "plus", Size: "md", Tone: "neutral"})}}, ButtonWithSlots),

		ExampleWithSlots(info("pk-ui.component.badge/default", "Status", "Badge / default"), BadgeProps{Label: "Default"}, BadgeSlots{}, BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/brand-dot", "Status", "Badge / brand dot"),
			BadgeProps{Label: "New", Variant: "primary", Tone: "brand", Dot: true}, BadgeSlots{}, BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/success", "Status", "Badge / success"), BadgeProps{Label: "OK", Tone: "success"}, BadgeSlots{}, BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/warning", "Status", "Badge / warning"),
			BadgeProps{Label: "Careful", Tone: "warning"}, BadgeSlots{IconStart: []g.Node{Icon(IconProps{Name: "warning", Size: "sm", Tone: "warning"})}}, BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/danger", "Status", "Badge / danger"), BadgeProps{Label: "Bad", Tone: "danger"}, BadgeSlots{}, BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/info", "Status", "Badge / info"), BadgeProps{Label: "FYI", Tone: "info"}, BadgeSlots{}, BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/secondary", "Status", "Badge / secondary"), BadgeProps{Label: "Two", Variant: "secondary"}, BadgeSlots{}, BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/outline", "Status", "Badge / outline"), BadgeProps{Label: "Outlined", Variant: "outline"}, BadgeSlots{}, BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/count", "Status", "Badge / count"), BadgeProps{Label: "Messages", Count: 120, Removable: true, Live: true}, BadgeSlots{}, BadgeWithSlots),

		ExampleOf(info("pk-ui.component.alert/success", "Status", "Alert / success"), AlertProps{Title: "Saved", Message: "All good.", Tone: "success"}, Alert),
		ExampleWithSlots(info("pk-ui.component.alert/warning", "Status", "Alert / warning"), AlertProps{Message: "Careful now.", Tone: "warning"},
			struct{ IconStart, Actions []g.Node }{IconStart: []g.Node{Icon(IconProps{Name: "warning", Size: "md", Tone: "warning"})}, Actions: nil},
			func(p AlertProps, slots struct{ IconStart, Actions []g.Node }) g.Node {
				return alertWithSlots(p, slots.IconStart, slots.Actions)
			}),
		ExampleOf(info("pk-ui.component.alert/danger", "Status", "Alert / danger"), AlertProps{Message: "That failed.", Tone: "danger"}, Alert),
		ExampleOf(info("pk-ui.component.alert/info", "Status", "Alert / info"), AlertProps{Message: "Heads up.", Tone: "info"}, Alert),
		ExampleOf(info("pk-ui.component.alert/compact", "Status", "Alert / compact"), AlertProps{Message: "Compact.", Tone: "info", Compact: true}, Alert),
		ExampleOf(info("pk-ui.component.alert/bordered", "Status", "Alert / bordered"), AlertProps{Message: "Accented.", Tone: "warning", Bordered: true}, Alert),
		ExampleOf(info("pk-ui.component.alert/dismissible", "Status", "Alert / dismissible"), AlertProps{Message: "Dismiss me.", Tone: "success", Dismissible: true}, Alert),

		ExampleOf(info("pk-ui.component.spinner/brand", "Status", "Spinner / brand"), SpinnerProps{Size: "sm", Tone: "brand"}, Spinner),
		ExampleOf(info("pk-ui.component.spinner/labelled", "Status", "Spinner / labelled"), SpinnerProps{Size: "md", Tone: "info", Label: "Fetching"}, Spinner),
		ExampleOf(info("pk-ui.component.spinner/success", "Status", "Spinner / success"), SpinnerProps{Size: "lg", Tone: "success"}, Spinner),

		ExampleOf(info("pk-ui.component.skeleton/block", "Status", "Skeleton / block"), SkeletonProps{}, Skeleton),
		ExampleOf(info("pk-ui.component.skeleton/block-sm", "Status", "Skeleton / block sm"), SkeletonProps{Shape: "block", Size: "sm"}, Skeleton),
		ExampleOf(info("pk-ui.component.skeleton/block-lg", "Status", "Skeleton / block lg"), SkeletonProps{Shape: "block", Size: "lg"}, Skeleton),
		ExampleOf(info("pk-ui.component.skeleton/text", "Status", "Skeleton / text"), SkeletonProps{Shape: "text", Lines: 3, Size: "sm"}, Skeleton),
		ExampleOf(info("pk-ui.component.skeleton/circle", "Status", "Skeleton / circle"), SkeletonProps{Shape: "circle"}, Skeleton),
		ExampleOf(info("pk-ui.component.tableskeleton/table", "Status", "Skeleton / table"), TableSkeletonProps{}, TableSkeleton),
		ExampleOf(info("pk-ui.component.tableskeleton/table-compact", "Status", "Skeleton / table compact"), TableSkeletonProps{Columns: 2, Rows: 5, Compact: true}, TableSkeleton),

		ExampleWithSlots(info("pk-ui.component.emptystate/default", "Status", "EmptyState"),
			EmptyStateProps{Title: "No tenants yet", Description: "Create the first tenant to get started.", Bordered: true},
			struct{ IconStart, Actions []g.Node }{IconStart: nil, Actions: []g.Node{Link(LinkProps{Label: "New tenant", Href: "/admin/tenants/new"})}},
			func(p EmptyStateProps, slots struct{ IconStart, Actions []g.Node }) g.Node {
				return emptyStateWithSlots(p, slots.IconStart, slots.Actions)
			}),
		ExampleOf(info("pk-ui.component.emptystate/compact", "Status", "EmptyState / compact"), EmptyStateProps{Title: "Empty", Compact: true}, EmptyState),

		ExampleOf(info("pk-ui.component.label/default", "Form", "Label"), LabelProps{Text: "Standalone", For: "x", Required: true}, Label),
		ExampleOf(info("pk-ui.component.input/email", "Form", "Input / email"), InputProps{Name: "email", Type: "email", Label: "Email",
			Placeholder: "you@example.test", HelpText: "We never share it.", Required: true}, Input),
		ExampleOf(info("pk-ui.component.input/invalid", "Form", "Input / invalid"), InputProps{Name: "slug", Label: "Slug", Value: "hello",
			Error: "Already taken.", Pattern: "[a-z-]+"}, Input),
		ExampleOf(info("pk-ui.component.input/bare", "Form", "Input / bare"), InputProps{Name: "quiet"}, Input),
		ExampleOf(info("pk-ui.component.input/read-only", "Form", "Input / read-only"), InputProps{Name: "id", Label: "Id", Value: "42", ReadOnly: true}, Input),
		ExampleOf(info("pk-ui.component.input/file", "Form", "Input / file"), InputProps{Name: "cover", Type: "file", Label: "Cover image",
			Accept: "image/png,image/jpeg", HelpText: "PNG or JPEG. The form it sits in is multipart."}, Input),
		ExampleOf(info("pk-ui.component.input/file-multiple", "Form", "Input / file multiple"), InputProps{Name: "attachments", Type: "file",
			Label: "Attachments", Accept: "image/*", Multiple: true}, Input),
		ExampleOf(info("pk-ui.component.select/default", "Form", "Select"), SelectProps{Name: "kind", Label: "Kind", Required: true,
			Options: []SelectOption{{Label: "Post", Value: "post"}, {Label: "Page", Value: "page"}},
			Value:   "post", HelpText: "What the entry renders as."}, Select),
		ExampleOf(info("pk-ui.component.select/invalid", "Form", "Select / invalid"), SelectProps{Name: "state", Label: "State", Placeholder: "Any state",
			Options: []SelectOption{{Label: "Draft", Value: "draft"}}, Error: "Pick a state."}, Select),
		ExampleOf(info("pk-ui.component.textarea/default", "Form", "Textarea"), TextareaProps{Name: "body", Label: "Body", Rows: 6,
			HelperText: "Markdown is fine.", MaxLength: 500}, Textarea),
		ExampleOf(info("pk-ui.component.textarea/invalid", "Form", "Textarea / invalid"), TextareaProps{Name: "bad", Label: "Bad", ErrorMessage: "Too long."}, Textarea),
		ExampleOf(info("pk-ui.component.textarea/autoresize", "Form", "Textarea / autoresize"), TextareaProps{
			ComponentProps: ComponentProps{Disabled: true},
			Name:           "details", Label: "Details", Value: "Existing", HelperText: "Add context.",
			ErrorMessage: "More detail is required.", AutoResize: true, MinRows: 3, MaxRows: 15, FullWidth: true}, Textarea),
		ExampleOf(info("pk-ui.component.checkbox/default", "Form", "Checkbox"), CheckboxProps{Name: "agree", Label: "I agree", Required: true}, Checkbox),
		ExampleOf(info("pk-ui.component.checkbox/checked", "Form", "Checkbox / checked"), CheckboxProps{Name: "done", Label: "Done", Checked: true}, Checkbox),
		ExampleOf(info("pk-ui.component.checkbox/indeterminate", "Form", "Checkbox / indeterminate"), CheckboxProps{Name: "some", Label: "Some", Indeterminate: true}, Checkbox),
		ExampleOf(info("pk-ui.component.checkbox/disabled", "Form", "Checkbox / disabled"), CheckboxProps{
			ComponentProps: ComponentProps{Disabled: true}, Name: "disabled", Label: "Disabled"}, Checkbox),

		ExampleWithChildren(info("pk-ui.component.stack/default", "Layout", "Stack"), StackProps{Gap: "2", Align: "start"}, []g.Node{g.Text("a"), g.Text("b")}, Stack),
		ExampleWithChildren(info("pk-ui.component.flex/default", "Layout", "Flex"), FlexProps{Direction: "row", Gap: "4", Align: "center",
			Justify: "between", Wrap: true}, []g.Node{g.Text("l"), g.Text("r")}, Flex),
		ExampleWithChildren(info("pk-ui.component.grid/default", "Layout", "Grid"), GridProps{Columns: "3", Gap: "6"}, []g.Node{g.Text("1"), g.Text("2"), g.Text("3")}, Grid),
		ExampleWithChildren(info("pk-ui.component.container/default", "Layout", "Container"), ContainerProps{MaxWidth: "4xl"}, []g.Node{g.Text("content")}, Container),
		ExampleWithSlots(info("pk-ui.component.card/default", "Layout", "Card"), CardProps{Title: "Plain card", Description: "With copy."}, CardSlots{}, CardWithSlots),
		ExampleWithSlots(info("pk-ui.component.card/clickable", "Layout", "Card / clickable"), CardProps{Title: "Go somewhere", Clickable: true, Href: "/detail"}, CardSlots{}, CardWithSlots),

		ExampleWithSlots(info("pk-ui.component.table/default", "Data", "Table"), TableProps{
			Columns: []TableColumn{{Key: "name", Label: "Name"}, {Key: "role", Label: "Role"}},
			Rows: []TableRow{
				{ID: "u1", Cells: map[string]any{"name": "Ada", "role": "admin"}},
				{ID: "u2", Cells: map[string]any{"name": "Lin", "role": 7}},
			}}, TableSlots{}, TableWithSlots),
		ExampleWithSlots(info("pk-ui.component.table/empty", "Data", "Table / empty"), TableProps{
			Columns: []TableColumn{{Key: "a", Label: "A"}}, EmptyText: "No rows.", Compact: true}, TableSlots{}, TableWithSlots),
		ExampleWithSlots(info("pk-ui.component.table/sortable", "Data", "Table / sortable"), TableProps{
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
			}}, TableSlots{}, TableWithSlots),
		ExampleOf(info("pk-ui.component.detaillist/default", "Data", "DetailList"), DetailListProps{
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
			}}, DetailList),

		ExampleOf(info("pk-ui.component.breadcrumb/default", "Navigation", "Breadcrumb"), BreadcrumbProps{Items: []BreadcrumbItem{
			{Label: "Home", Href: "/"}, {Label: "Tenants", Href: "/tenants"}, {Label: "Acme"},
		}}, Breadcrumb),
		// Each landmark on this page needs a name of its own: two navigations
		// both called "Pagination" is a screen reader offering the same
		// destination twice. It is the gallery's problem and not the
		// component's, so it is solved with the prop the component already has.
		ExampleOf(info("pk-ui.component.pagination/default", "Navigation", "Pagination"), PaginationProps{
			CurrentPage: 5, TotalPages: 12, BaseURL: "/rows", NavigationLabel: "Pagination, twelve pages"}, Pagination),
		ExampleOf(info("pk-ui.component.pagination/two-pages", "Navigation", "Pagination / two pages"), PaginationProps{
			CurrentPage: 1, TotalPages: 2, BaseURL: "/few", NavigationLabel: "Pagination, two pages"}, Pagination),
		ExampleOf(info("pk-ui.component.tabs/default", "Navigation", "Tabs"), TabsProps{ActiveTab: "b", Items: []TabItem{
			{Key: "a", Label: "First", URL: "/tab/a"}, {Key: "b", Label: "Second"},
		}}, Tabs),
		ExampleWithSlots(info("pk-ui.component.tabs/vertical-pills", "Navigation", "Tabs / vertical pills"),
			TabsProps{ActiveTab: "profile", Orientation: "vertical", Variant: "pills"}, TabsSlots{Tabs: []TabSlot{
				{ID: "profile", Label: "Profile", Icon: "user", Badge: "New", Content: []g.Node{g.Text("Profile panel")}},
				{ID: "security", Label: "Security", Disabled: true, Content: []g.Node{g.Text("Security panel")}},
				{ID: "activity", Label: "Activity", HxGet: "/activity"},
			}}, TabsWithSlots),
		ExampleWithSlots(info("pk-ui.component.sidebar/default", "Navigation", "Sidebar"), SidebarProps{
			NavigationLabel: "Sidebar example, sections",
			Current:         "/admin/customers/accounts",
			Sections: []SidebarSection{{
				ID: "operate", Label: "Operate", Glyph: "O", Tone: "brand",
				Items: []SidebarItem{
					{Label: "Dashboard", Href: "/admin", Icon: "gear"},
					{Label: "Customers", Href: "/admin/customers", Icon: "user", Badge: "24", Children: []SidebarItem{
						{Label: "Accounts", Href: "/admin/customers/accounts"},
					}},
				},
			}},
		}, SidebarSlots{Footer: []g.Node{g.Text("Signed in")}}, SidebarWithSlots),
		ExampleWithSlots(info("pk-ui.component.sidebar/content-flavor", "Navigation", "Sidebar / content flavor"), SidebarProps{
			ComponentProps: ComponentProps{ID: "docs-sidebar"},
			Flavor:         "content", Current: "#two", NavigationLabel: "Documentation sections",
			Sections: []SidebarSection{{
				ID: "runtime", Label: "Runtime", Glyph: "R", Tone: "info", SearchText: "runtime docs",
				Items: []SidebarItem{
					{ID: "one", Label: "Overview", Href: "#one", Prefix: "01", SearchText: "overview"},
					{ID: "two", Label: "Handoff", Href: "#two", Prefix: "02", SearchText: "handoff"},
				},
			}},
		}, SidebarSlots{Brand: []g.Node{h.Strong(g.Text("Documentation"))}, Footer: []g.Node{g.Text("Version 1")}}, SidebarWithSlots),
		ExampleWithSlots(info("pk-ui.component.sidebar/collapsed", "Navigation", "Sidebar / collapsed"), SidebarProps{
			ComponentProps:  ComponentProps{Disabled: true},
			NavigationLabel: "Sidebar example, collapsed",
			Collapsible:     true, Collapsed: true,
			Items: []SidebarItem{
				{Label: "Home", Href: "/admin", Icon: "gear"},
				{Label: "Reports", Href: "/admin/reports", Icon: "file-text", Disabled: true},
			}}, SidebarSlots{}, SidebarWithSlots),

		ExampleWithSlots(info("pk-ui.component.modal/default", "Overlay", "Modal"), ModalProps{ComponentProps: ComponentProps{ID: "confirm-modal"},
			Title: "Archive", Description: "This action cannot be undone.",
			Body: "Review the affected records.", Footer: "Confirm or cancel.", Size: "small", Open: true}, ModalSlots{}, ModalWithSlots),
		ExampleWithSlots(info("pk-ui.component.modal/medium", "Overlay", "Modal / medium"), ModalProps{Title: "Edit record", Size: "medium"}, ModalSlots{}, ModalWithSlots),
		ExampleWithSlots(info("pk-ui.component.modal/large", "Overlay", "Modal / large"), ModalProps{Title: "Large review", Size: "large"}, ModalSlots{}, ModalWithSlots),
		ExampleWithSlots(info("pk-ui.component.modal/xl", "Overlay", "Modal / xl"), ModalProps{Title: "Wide review", Size: "xl"}, ModalSlots{}, ModalWithSlots),
		ExampleWithSlots(info("pk-ui.component.modal/undismissable", "Overlay", "Modal / undismissable"), ModalProps{AriaLabel: "Required decision", Size: "full",
			Closable: new(false), CloseOnOverlay: new(false), CloseOnEscape: new(false), ShowClose: new(false), ShowOverlay: new(false), Centered: new(false)}, ModalSlots{}, ModalWithSlots),
		ExampleWithSlots(info("pk-ui.component.modal/deferred", "Overlay", "Modal / deferred"), ModalProps{ComponentProps: ComponentProps{ID: "server-modal"},
			AriaLabel: "Server dialog", Deferred: true, OpenOnSwap: true}, ModalSlots{}, ModalWithSlots),
		ExampleWithSlots(info("pk-ui.component.modal/panel", "Overlay", "Modal / panel"), ModalProps{Title: "Panel only"}, struct{ Body g.Node }{Body: g.Text("Body")},
			func(p ModalProps, slots struct{ Body g.Node }) g.Node { return ModalPanel(p, slots.Body) }),
		ExamplePreview(info("pk-ui.component.modal/form", "Overlay", "Modal / form"), ModalForm(g.Text("Fields")), "ModalForm accepts arbitrary nodes rather than typed properties."),
		ExamplePreview(info("pk-ui.component.modal/close-button", "Overlay", "Modal / close button"),
			ModalCloseButton("Close", ""), "ModalCloseButton accepts strings rather than a typed properties contract."),
		ExamplePreview(info("pk-ui.component.modal/cancel-button", "Overlay", "Modal / cancel button"),
			ModalCancelButton("Cancel", ""), "ModalCancelButton accepts strings rather than a typed properties contract."),
		ExampleOf(info("pk-ui.component.confirmdialog/default", "Overlay", "ConfirmDialog"), ConfirmDialogProps{
			Title: "Delete this row?", AcceptLabel: "Delete", CancelLabel: "Keep"}, ConfirmDialog),

		ExamplePreview(info("pk-ui.component.skiplink/default", "Frame", "SkipLink"), SkipLink("content", "Skip to content"), "SkipLink accepts strings rather than a typed properties contract."),
		ExampleWithChildren(info("pk-ui.component.toolbar/default", "Frame", "Toolbar"),
			ToolbarProps{Title: "Tasks", Subtitle: "12 records"}, []g.Node{Button(ButtonProps{Label: "New task", Href: "/admin/task/tasks/new"})}, Toolbar),
		ExampleWithChildren(info("pk-ui.component.form/default", "Frame", "Form"),
			FormProps{Action: "/admin/task/tasks", Label: "New task"}, []g.Node{Input(InputProps{Name: "title", Label: "Title", Required: true}), FormActions(FormActionsProps{},
				Button(ButtonProps{Label: "Cancel", Variant: "secondary", Href: "/admin/task/tasks"}),
				Button(ButtonProps{Label: "Create", Type: "submit"}))}, Form),
		// Shell is not in this list, and cannot be: it renders <main>, and the
		// page this gallery is on is itself a Shell, so an example would be a
		// second main landmark inside the first — two documents in one, which
		// is exactly what the landmark rules exist to catch. The page is the
		// example. Its classes are covered by modules/admin's own closure test,
		// which renders every real screen and checks each class against the
		// stylesheet.
	}
}
