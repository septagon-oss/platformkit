package examples

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
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/components"
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
		ExampleOf(info("pk-ui.component.heading/display", "Sections", "Heading / independent size"), components.HeadingProps{Text: "A section with presence", Level: 2, Size: 1}, components.Heading),
		ExampleOf(info("pk-ui.component.section-header/default", "Sections", "Section header"), components.SectionHeaderProps{Eyebrow: "Your workspace", Title: "Everything in its place", Description: "A shared introduction for composed pages.", Level: 2}, components.SectionHeader),
		ExampleWithSlots(info("pk-ui.component.section/default", "Sections", "Section"), components.SectionProps{MaxWidth: "lg"}, components.SectionSlots{Header: []g.Node{components.Heading(components.HeadingProps{Text: "Latest activity", Level: 2})}, Body: []g.Node{components.Text(components.TextProps{Content: "Compose your own content inside a shared section."})}}, components.Section),
		ExampleWithSlots(info("pk-ui.component.hero/default", "Sections", "Hero"), components.HeroProps{SectionHeaderProps: components.SectionHeaderProps{Eyebrow: "PlatformKit", Title: "Build your next workspace", Description: "Shared components. Your composition."}}, components.HeroSlots{Actions: []g.Node{components.Button(components.ButtonProps{Label: "Get started"})}, Media: []g.Node{components.Card(components.CardProps{Title: "One composition", Description: "This region accepts your product's media or content."})}}, components.Hero),
		ExampleWithChildren(info("pk-ui.component.grid/responsive", "Sections", "Grid / responsive"), components.GridProps{Columns: "1", SM: "2", LG: "3", Gap: "4"}, []g.Node{components.Card(components.CardProps{Title: "Plan"}), components.Card(components.CardProps{Title: "Build"}), components.Card(components.CardProps{Title: "Review"})}, components.Grid),
		ExampleOf(info("pk-ui.component.video/default", "Media", "Video / captions"), components.VideoProps{Label: "Recorded instruction", Sources: []components.VideoSource{{Src: "/example.mp4", Type: "video/mp4"}}, Tracks: []components.VideoTrack{{Src: "/example.vtt", Language: "en", Label: "English", Default: true}}}, components.Video),
		ExampleOf(info("pk-ui.component.video/disabled", "Media", "Video / unavailable"), components.VideoProps{ComponentProps: components.ComponentProps{Disabled: true}, Label: "Recording unavailable"}, components.Video),
		ExampleOf(info("pk-ui.component.heading/1", "Type", "Heading / 1"), components.HeadingProps{Text: "Page title", Level: 1}, components.Heading),
		ExampleOf(info("pk-ui.component.heading/2", "Type", "Heading / 2"), components.HeadingProps{Text: "Section", Level: 2, Anchor: "section"}, components.Heading),
		ExampleOf(info("pk-ui.component.heading/3", "Type", "Heading / 3"), components.HeadingProps{Text: "Sub", Level: 3}, components.Heading),
		ExampleOf(info("pk-ui.component.heading/4", "Type", "Heading / 4"), components.HeadingProps{Text: "Minor", Level: 4}, components.Heading),
		ExampleOf(info("pk-ui.component.heading/5", "Type", "Heading / 5"), components.HeadingProps{Text: "Small", Level: 5}, components.Heading),
		ExampleOf(info("pk-ui.component.heading/6", "Type", "Heading / 6"), components.HeadingProps{Text: "Eyebrow", Level: 6}, components.Heading),
		ExampleOf(info("pk-ui.component.text/muted", "Type", "Text / muted"), components.TextProps{Content: "Plain body copy.", Size: "sm", Color: "muted"}, components.Text),
		ExampleOf(info("pk-ui.component.text/loud", "Type", "Text / loud"), components.TextProps{Content: "Loud.", Size: "xl", Weight: "bold", Color: "brand"}, components.Text),
		ExampleOf(info("pk-ui.component.text/truncated", "Type", "Text / truncated"), components.TextProps{Content: "Cut off eventually.", Truncate: true}, components.Text),
		ExampleOf(info("pk-ui.component.link/internal", "Type", "Link / internal"), components.LinkProps{Label: "Internal", Href: "/docs"}, components.Link),
		ExampleOf(info("pk-ui.component.link/external", "Type", "Link / external"), components.LinkProps{Label: "External", Href: "https://example.test", External: true}, components.Link),
		ExampleOf(info("pk-ui.component.divider/default", "Type", "Divider"), components.DividerProps{}, components.Divider),
		ExampleOf(info("pk-ui.component.divider/labelled", "Type", "Divider / labelled"), components.DividerProps{Text: "Or continue with"}, components.Divider),
		ExampleOf(info("pk-ui.component.divider/vertical", "Type", "Divider / vertical"), components.DividerProps{Orientation: "vertical"}, components.Divider),

		ExampleOf(info("pk-ui.component.icon/check", "Icon", "Icon / check"), components.IconProps{Name: "check", Size: "lg", Tone: "success"}, components.Icon),
		ExampleOf(info("pk-ui.component.icon/trash", "Icon", "Icon / trash"), components.IconProps{Name: "trash", Size: "md", Tone: "danger"}, components.Icon),
		ExampleOf(info("pk-ui.component.icon/unknown", "Icon", "Icon / unknown"), components.IconProps{Name: "no-such-glyph"}, components.Icon),

		ExampleWithSlots(info("pk-ui.component.button/primary", "Action", "Button / primary"),
			components.ButtonProps{Label: "Save", Variant: "primary", Tone: "neutral", Size: "md"}, components.ButtonSlots{}, components.ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/secondary", "Action", "Button / secondary"),
			components.ButtonProps{Label: "Cancel", Variant: "secondary", Tone: "neutral", Size: "sm"}, components.ButtonSlots{}, components.ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/danger", "Action", "Button / danger"),
			components.ButtonProps{Label: "Delete", Variant: "primary", Tone: "danger", Size: "xs"}, components.ButtonSlots{}, components.ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/ghost", "Action", "Button / ghost"),
			components.ButtonProps{Label: "Ghost", Variant: "ghost", Tone: "neutral", Size: "lg"}, components.ButtonSlots{}, components.ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/link", "Action", "Button / link"), components.ButtonProps{Label: "Docs", Variant: "link", Tone: "neutral", Size: "xl"}, components.ButtonSlots{}, components.ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/outline-full", "Action", "Button / outline full"),
			components.ButtonProps{Label: "Outline", Variant: "outline", Tone: "neutral", Size: "2xl", FullWidth: true}, components.ButtonSlots{}, components.ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/info", "Action", "Button / info"), components.ButtonProps{Label: "Info", Variant: "primary", Tone: "info"}, components.ButtonSlots{}, components.ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/warning", "Action", "Button / warning"), components.ButtonProps{Label: "Warn", Variant: "primary", Tone: "warning"}, components.ButtonSlots{}, components.ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/success", "Action", "Button / success"), components.ButtonProps{Label: "OK", Variant: "primary", Tone: "success"}, components.ButtonSlots{}, components.ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/loading", "Action", "Button / loading"), components.ButtonProps{Label: "Busy", Loading: true}, components.ButtonSlots{}, components.ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/as-link", "Action", "Button / as link"), components.ButtonProps{Label: "Open", Href: "/somewhere"}, components.ButtonSlots{}, components.ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/disabled-link", "Action", "Button / disabled link"),
			components.ButtonProps{ComponentProps: components.ComponentProps{Disabled: true}, HTMXProps: components.HTMXProps{Get: "/admin", Boost: true}, Label: "Unavailable", Href: "/admin"}, components.ButtonSlots{}, components.ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/with-icon", "Action", "Button / with icon"),
			components.ButtonProps{Label: "Iconed", Variant: "primary", Tone: "neutral", Size: "md"}, components.ButtonSlots{IconEnd: []g.Node{
				ExampleOf(ExampleInfo{ID: "icon", ComponentID: "pk-ui.component.icon"}, components.IconProps{Name: "plus", Size: "md", Tone: "neutral"}, components.Icon).Node,
			}}, components.ButtonWithSlots),
		ExampleWithSlots(info("pk-ui.component.button/with-leading-icon", "Action", "Button / leading icon"),
			components.ButtonProps{Label: "Add item", Variant: "primary", Tone: "neutral", Size: "md"}, components.ButtonSlots{IconStart: []g.Node{
				ExampleOf(ExampleInfo{ID: "icon", ComponentID: "pk-ui.component.icon"}, components.IconProps{Name: "plus", Size: "sm", Tone: "neutral"}, components.Icon).Node,
			}}, components.ButtonWithSlots),

		ExampleWithSlots(info("pk-ui.component.badge/default", "Status", "Badge / default"), components.BadgeProps{Label: "Default"}, components.BadgeSlots{}, components.BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/brand-dot", "Status", "Badge / brand dot"),
			components.BadgeProps{Label: "New", Variant: "primary", Tone: "brand", Dot: true}, components.BadgeSlots{}, components.BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/success", "Status", "Badge / success"), components.BadgeProps{Label: "OK", Tone: "success"}, components.BadgeSlots{}, components.BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/warning", "Status", "Badge / warning"),
			components.BadgeProps{Label: "Careful", Tone: "warning"}, components.BadgeSlots{IconStart: []g.Node{
				ExampleOf(ExampleInfo{ID: "icon", ComponentID: "pk-ui.component.icon"}, components.IconProps{Name: "warning", Size: "sm", Tone: "warning"}, components.Icon).Node,
			}}, components.BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/danger", "Status", "Badge / danger"), components.BadgeProps{Label: "Bad", Tone: "danger"}, components.BadgeSlots{}, components.BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/info", "Status", "Badge / info"), components.BadgeProps{Label: "FYI", Tone: "info"}, components.BadgeSlots{}, components.BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/secondary", "Status", "Badge / secondary"), components.BadgeProps{Label: "Two", Variant: "secondary"}, components.BadgeSlots{}, components.BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/outline", "Status", "Badge / outline"), components.BadgeProps{Label: "Outlined", Variant: "outline"}, components.BadgeSlots{}, components.BadgeWithSlots),
		ExampleWithSlots(info("pk-ui.component.badge/count", "Status", "Badge / count"), components.BadgeProps{Label: "Messages", Count: 120, Removable: true, Live: true}, components.BadgeSlots{}, components.BadgeWithSlots),

		ExampleWithSlots(info("pk-ui.component.alert/success", "Status", "Alert / success"), components.AlertProps{Title: "Saved", Message: "All good.", Tone: "success"}, components.AlertSlots{}, components.AlertWithSlots),
		ExampleWithSlots(info("pk-ui.component.alert/warning", "Status", "Alert / warning"), components.AlertProps{Message: "Careful now.", Tone: "warning"},
			components.AlertSlots{IconStart: []g.Node{
				ExampleOf(ExampleInfo{ID: "icon", ComponentID: "pk-ui.component.icon"}, components.IconProps{Name: "warning", Size: "md", Tone: "warning"}, components.Icon).Node,
			}}, components.AlertWithSlots),
		ExampleWithSlots(info("pk-ui.component.alert/danger", "Status", "Alert / danger"), components.AlertProps{Message: "That failed.", Tone: "danger"}, components.AlertSlots{}, components.AlertWithSlots),
		ExampleWithSlots(info("pk-ui.component.alert/info", "Status", "Alert / info"), components.AlertProps{Message: "Heads up.", Tone: "info"}, components.AlertSlots{}, components.AlertWithSlots),
		ExampleWithSlots(info("pk-ui.component.alert/compact", "Status", "Alert / compact"), components.AlertProps{Message: "Compact.", Tone: "info", Compact: true}, components.AlertSlots{}, components.AlertWithSlots),
		ExampleWithSlots(info("pk-ui.component.alert/bordered", "Status", "Alert / bordered"), components.AlertProps{Message: "Accented.", Tone: "warning", Bordered: true}, components.AlertSlots{}, components.AlertWithSlots),
		ExampleWithSlots(info("pk-ui.component.alert/dismissible", "Status", "Alert / dismissible"), components.AlertProps{Message: "Dismiss me.", Tone: "success", Dismissible: true}, components.AlertSlots{}, components.AlertWithSlots),

		ExampleOf(info("pk-ui.component.spinner/brand", "Status", "Spinner / brand"), components.SpinnerProps{Size: "sm", Tone: "brand"}, components.Spinner),
		ExampleOf(info("pk-ui.component.spinner/labelled", "Status", "Spinner / labelled"), components.SpinnerProps{Size: "md", Tone: "info", Label: "Fetching"}, components.Spinner),
		ExampleOf(info("pk-ui.component.spinner/success", "Status", "Spinner / success"), components.SpinnerProps{Size: "lg", Tone: "success"}, components.Spinner),

		ExampleOf(info("pk-ui.component.skeleton/block", "Status", "Skeleton / block"), components.SkeletonProps{}, components.Skeleton),
		ExampleOf(info("pk-ui.component.skeleton/block-sm", "Status", "Skeleton / block sm"), components.SkeletonProps{Shape: "block", Size: "sm"}, components.Skeleton),
		ExampleOf(info("pk-ui.component.skeleton/block-lg", "Status", "Skeleton / block lg"), components.SkeletonProps{Shape: "block", Size: "lg"}, components.Skeleton),
		ExampleOf(info("pk-ui.component.skeleton/text", "Status", "Skeleton / text"), components.SkeletonProps{Shape: "text", Lines: 3, Size: "sm"}, components.Skeleton),
		ExampleOf(info("pk-ui.component.skeleton/circle", "Status", "Skeleton / circle"), components.SkeletonProps{Shape: "circle"}, components.Skeleton),
		ExampleOf(info("pk-ui.component.tableskeleton/table", "Status", "Skeleton / table"), components.TableSkeletonProps{}, components.TableSkeleton),
		ExampleOf(info("pk-ui.component.tableskeleton/table-compact", "Status", "Skeleton / table compact"), components.TableSkeletonProps{Columns: 2, Rows: 5, Compact: true}, components.TableSkeleton),

		ExampleWithSlots(info("pk-ui.component.emptystate/default", "Status", "EmptyState"),
			components.EmptyStateProps{Title: "No tenants yet", Description: "Create the first tenant to get started.", Bordered: true},
			components.EmptyStateSlots{Actions: []g.Node{
				ExampleOf(ExampleInfo{ID: "action", ComponentID: "pk-ui.component.link"}, components.LinkProps{Label: "New tenant", Href: "/admin/tenants/new"}, components.Link).Node,
			}}, components.EmptyStateWithSlots),
		ExampleWithSlots(info("pk-ui.component.emptystate/compact", "Status", "EmptyState / compact"), components.EmptyStateProps{Title: "Empty", Compact: true}, components.EmptyStateSlots{}, components.EmptyStateWithSlots),

		ExampleOf(info("pk-ui.component.label/default", "Form", "Label"), components.LabelProps{Text: "Standalone", For: "x", Required: true}, components.Label),
		ExampleOf(info("pk-ui.component.input/email", "Form", "Input / email"), components.InputProps{Name: "email", Type: "email", Label: "Email",
			Placeholder: "you@example.test", HelpText: "We never share it.", Required: true}, components.Input),
		ExampleOf(info("pk-ui.component.input/invalid", "Form", "Input / invalid"), components.InputProps{Name: "slug", Label: "Slug", Value: "hello",
			Error: "Already taken.", Pattern: "[a-z-]+"}, components.Input),
		ExampleOf(info("pk-ui.component.input/bare", "Form", "Input / bare"), components.InputProps{Name: "quiet"}, components.Input),
		ExampleOf(info("pk-ui.component.input/read-only", "Form", "Input / read-only"), components.InputProps{Name: "id", Label: "Id", Value: "42", ReadOnly: true}, components.Input),
		ExampleOf(info("pk-ui.component.input/file", "Form", "Input / file"), components.InputProps{Name: "cover", Type: "file", Label: "Cover image",
			Accept: "image/png,image/jpeg", HelpText: "PNG or JPEG. The form it sits in is multipart."}, components.Input),
		ExampleOf(info("pk-ui.component.input/file-multiple", "Form", "Input / file multiple"), components.InputProps{Name: "attachments", Type: "file",
			Label: "Attachments", Accept: "image/*", Multiple: true}, components.Input),
		ExampleOf(info("pk-ui.component.select/default", "Form", "Select"), components.SelectProps{Name: "kind", Label: "Kind", Required: true,
			Options: []components.SelectOption{{Label: "Post", Value: "post"}, {Label: "Page", Value: "page"}},
			Value:   "post", HelpText: "What the entry renders as."}, components.Select),
		ExampleOf(info("pk-ui.component.select/invalid", "Form", "Select / invalid"), components.SelectProps{Name: "state", Label: "State", Placeholder: "Any state",
			Options: []components.SelectOption{{Label: "Draft", Value: "draft"}}, Error: "Pick a state."}, components.Select),
		ExampleOf(info("pk-ui.component.textarea/default", "Form", "Textarea"), components.TextareaProps{Name: "body", Label: "Body", Rows: 6,
			HelperText: "Markdown is fine.", MaxLength: 500}, components.Textarea),
		ExampleOf(info("pk-ui.component.textarea/invalid", "Form", "Textarea / invalid"), components.TextareaProps{Name: "bad", Label: "Bad", ErrorMessage: "Too long."}, components.Textarea),
		ExampleOf(info("pk-ui.component.textarea/autoresize", "Form", "Textarea / autoresize"), components.TextareaProps{
			ComponentProps: components.ComponentProps{Disabled: true},
			Name:           "details", Label: "Details", Value: "Existing", HelperText: "Add context.",
			ErrorMessage: "More detail is required.", AutoResize: true, MinRows: 3, MaxRows: 15, FullWidth: true}, components.Textarea),
		ExampleOf(info("pk-ui.component.checkbox/default", "Form", "Checkbox"), components.CheckboxProps{Name: "agree", Label: "I agree", Required: true}, components.Checkbox),
		ExampleOf(info("pk-ui.component.checkbox/checked", "Form", "Checkbox / checked"), components.CheckboxProps{Name: "done", Label: "Done", Checked: true}, components.Checkbox),
		ExampleOf(info("pk-ui.component.checkbox/indeterminate", "Form", "Checkbox / indeterminate"), components.CheckboxProps{Name: "some", Label: "Some", Indeterminate: true}, components.Checkbox),
		ExampleOf(info("pk-ui.component.checkbox/disabled", "Form", "Checkbox / disabled"), components.CheckboxProps{
			ComponentProps: components.ComponentProps{Disabled: true}, Name: "disabled", Label: "Disabled"}, components.Checkbox),

		ExampleWithChildren(info("pk-ui.component.stack/default", "Layout", "Stack"), components.StackProps{Gap: "2", Align: "start"}, []g.Node{g.Text("a"), g.Text("b")}, components.Stack),
		ExampleWithChildren(info("pk-ui.component.flex/default", "Layout", "Flex"), components.FlexProps{Direction: "row", Gap: "4", Align: "center",
			Justify: "between", Wrap: true}, []g.Node{g.Text("l"), g.Text("r")}, components.Flex),
		ExampleWithChildren(info("pk-ui.component.grid/default", "Layout", "Grid"), components.GridProps{Columns: "3", Gap: "6"}, []g.Node{
			ExampleOf(ExampleInfo{ID: "first", ComponentID: "pk-ui.component.text"}, components.TextProps{Content: "1"}, components.Text).Node,
			ExampleOf(ExampleInfo{ID: "second", ComponentID: "pk-ui.component.text"}, components.TextProps{Content: "2"}, components.Text).Node,
			ExampleOf(ExampleInfo{ID: "third", ComponentID: "pk-ui.component.text"}, components.TextProps{Content: "3"}, components.Text).Node,
		}, components.Grid),
		ExampleWithChildren(info("pk-ui.component.container/default", "Layout", "Container"), components.ContainerProps{MaxWidth: "4xl"}, []g.Node{g.Text("content")}, components.Container),
		ExampleWithSlots(info("pk-ui.component.card/default", "Layout", "Card"), components.CardProps{Title: "Plain card", Description: "With copy."}, components.CardSlots{}, components.CardWithSlots),
		ExampleWithSlots(info("pk-ui.component.card/clickable", "Layout", "Card / clickable"), components.CardProps{Title: "Go somewhere", Clickable: true, Href: "/detail"}, components.CardSlots{}, components.CardWithSlots),

		ExampleWithSlots(info("pk-ui.component.table/default", "Data", "Table"), components.TableProps{
			Columns: []components.TableColumn{{Key: "name", Label: "Name"}, {Key: "role", Label: "Role"}},
			Rows: []components.TableRow{
				{ID: "u1", Cells: map[string]any{"name": "Ada", "role": "admin"}},
				{ID: "u2", Cells: map[string]any{"name": "Lin", "role": 7}},
			}}, components.TableSlots{}, components.TableWithSlots),
		ExampleWithSlots(info("pk-ui.component.table/empty", "Data", "Table / empty"), components.TableProps{
			Columns: []components.TableColumn{{Key: "a", Label: "A"}}, EmptyText: "No rows.", Compact: true}, components.TableSlots{}, components.TableWithSlots),
		ExampleWithSlots(info("pk-ui.component.table/sortable", "Data", "Table / sortable"), components.TableProps{
			Sortable: true, Striped: true, Selectable: true,
			Columns: []components.TableColumn{
				{Key: "name", Label: "Name", Sortable: true, Primary: true},
				{Key: "count", Label: "Count", Sortable: true, Align: "right"},
				{Key: "note", Label: "Note"},
			},
			Rows: []components.TableRow{
				{ID: "r1", Cells: map[string]any{"name": "First", "count": 3, "note": "odd"}},
				{ID: "r2", Cells: map[string]any{"name": "Second", "count": 1, "note": "even"}},
				{ID: "r3", Cells: map[string]any{"name": "Third", "count": 2}},
			}}, components.TableSlots{}, components.TableWithSlots),
		ExampleOf(info("pk-ui.component.detaillist/default", "Data", "DetailList"), components.DetailListProps{
			ComponentProps: components.ComponentProps{ID: "account-facts"},
			Title:          "Profile",
			Description:    "Identity used across this workspace.",
			SemanticRole:   "identity",
			Items: []components.DetailItem{
				{Label: "Email", Value: "ada@example.test", Tone: "neutral"},
				{Label: "Plan", Value: "Studio", Description: "Renews next month.", Tone: "brand"},
				{Label: "Health", Value: "Good", Tone: "success"},
				{Label: "Review", Value: "Soon", Tone: "warning"},
				{Label: "Risk", Value: "High", Tone: "danger"},
				{Label: "Region", Value: "EU", Tone: "info"},
			}}, components.DetailList),

		ExampleOf(info("pk-ui.component.breadcrumb/default", "Navigation", "Breadcrumb"), components.BreadcrumbProps{Items: []components.BreadcrumbItem{
			{Label: "Home", Href: "/"}, {Label: "Tenants", Href: "/tenants"}, {Label: "Acme"},
		}}, components.Breadcrumb),
		// Each landmark on this page needs a name of its own: two navigations
		// both called "Pagination" is a screen reader offering the same
		// destination twice. It is the gallery's problem and not the
		// component's, so it is solved with the prop the component already has.
		ExampleOf(info("pk-ui.component.pagination/default", "Navigation", "Pagination"), components.PaginationProps{
			CurrentPage: 5, TotalPages: 12, BaseURL: "/rows", NavigationLabel: "Pagination, twelve pages"}, components.Pagination),
		ExampleOf(info("pk-ui.component.pagination/two-pages", "Navigation", "Pagination / two pages"), components.PaginationProps{
			CurrentPage: 1, TotalPages: 2, BaseURL: "/few", NavigationLabel: "Pagination, two pages"}, components.Pagination),
		ExampleOf(info("pk-ui.component.tabs/default", "Navigation", "Tabs"), components.TabsProps{ActiveTab: "b", Items: []components.TabItem{
			{Key: "a", Label: "First", URL: "/tab/a"}, {Key: "b", Label: "Second"},
		}}, components.Tabs),
		ExampleWithSlots(ExampleInfo{ID: "pk-ui.component.tabs/vertical-pills", ComponentID: "pk-ui.component.tabpanels", Group: "Navigation", Name: "Tabs / vertical pills"},
			components.TabsProps{ActiveTab: "profile", Orientation: "vertical", Variant: "pills"}, components.TabsSlots{Tabs: []components.TabSlot{
				{ID: "profile", Label: "Profile", Icon: "user", Badge: "New", Content: []g.Node{g.Text("Profile panel")}},
				{ID: "security", Label: "Security", Disabled: true, Content: []g.Node{g.Text("Security panel")}},
				{ID: "activity", Label: "Activity", HxGet: "/activity"},
			}}, components.TabsWithSlots),
		ExampleWithSlots(info("pk-ui.component.sidebar/default", "Navigation", "Sidebar"), components.SidebarProps{
			NavigationLabel: "Sidebar example, sections",
			Current:         "/admin/customers/accounts",
			Sections: []components.SidebarSection{{
				ID: "operate", Label: "Operate", Glyph: "O", Tone: "brand",
				Items: []components.SidebarItem{
					{Label: "Dashboard", Href: "/admin", Icon: "gear"},
					{Label: "Customers", Href: "/admin/customers", Icon: "user", Badge: "24", Children: []components.SidebarItem{
						{Label: "Accounts", Href: "/admin/customers/accounts"},
					}},
				},
			}},
		}, components.SidebarSlots{Footer: []g.Node{g.Text("Signed in")}}, components.SidebarWithSlots),
		ExampleWithSlots(info("pk-ui.component.sidebar/content-flavor", "Navigation", "Sidebar / content flavor"), components.SidebarProps{
			ComponentProps: components.ComponentProps{ID: "docs-sidebar"},
			Flavor:         "content", Current: "#two", NavigationLabel: "Documentation sections",
			Sections: []components.SidebarSection{{
				ID: "runtime", Label: "Runtime", Glyph: "R", Tone: "info", SearchText: "runtime docs",
				Items: []components.SidebarItem{
					{ID: "one", Label: "Overview", Href: "#one", Prefix: "01", SearchText: "overview"},
					{ID: "two", Label: "Handoff", Href: "#two", Prefix: "02", SearchText: "handoff"},
				},
			}},
		}, components.SidebarSlots{Brand: []g.Node{h.Strong(g.Text("Documentation"))}, Footer: []g.Node{g.Text("Version 1")}}, components.SidebarWithSlots),
		ExampleWithSlots(info("pk-ui.component.sidebar/collapsed", "Navigation", "Sidebar / collapsed"), components.SidebarProps{
			ComponentProps:  components.ComponentProps{Disabled: true},
			NavigationLabel: "Sidebar example, collapsed",
			Collapsible:     true, Collapsed: true,
			Items: []components.SidebarItem{
				{Label: "Home", Href: "/admin", Icon: "gear"},
				{Label: "Reports", Href: "/admin/reports", Icon: "file-text", Disabled: true},
			}}, components.SidebarSlots{}, components.SidebarWithSlots),

		ExampleWithSlots(info("pk-ui.component.modal/default", "Overlay", "Modal"), components.ModalProps{ComponentProps: components.ComponentProps{ID: "confirm-modal"},
			Title: "Archive", Description: "This action cannot be undone.",
			Body: "Review the affected records.", Footer: "Confirm or cancel.", Size: "small", Open: true}, components.ModalSlots{}, components.ModalWithSlots),
		ExampleWithSlots(info("pk-ui.component.modal/medium", "Overlay", "Modal / medium"), components.ModalProps{Title: "Edit record", Size: "medium", Open: true}, components.ModalSlots{}, components.ModalWithSlots),
		ExampleWithSlots(info("pk-ui.component.modal/large", "Overlay", "Modal / large"), components.ModalProps{Title: "Large review", Size: "large", Open: true}, components.ModalSlots{}, components.ModalWithSlots),
		ExampleWithSlots(info("pk-ui.component.modal/xl", "Overlay", "Modal / xl"), components.ModalProps{Title: "Wide review", Size: "xl", Open: true}, components.ModalSlots{}, components.ModalWithSlots),
		ExampleWithSlots(info("pk-ui.component.modal/undismissable", "Overlay", "Modal / undismissable"), components.ModalProps{AriaLabel: "Required decision", Size: "full", Open: true,
			Closable: new(false), CloseOnOverlay: new(false), CloseOnEscape: new(false), ShowClose: new(false), ShowOverlay: new(false), Centered: new(false)}, components.ModalSlots{}, components.ModalWithSlots),
		ExampleWithSlots(info("pk-ui.component.modal/deferred", "Overlay", "Modal / deferred"), components.ModalProps{ComponentProps: components.ComponentProps{ID: "server-modal"},
			AriaLabel: "Server dialog", Deferred: true, OpenOnSwap: true}, components.ModalSlots{}, components.ModalWithSlots),
		ExampleWithSlots(ExampleInfo{ID: "pk-ui.component.modal/panel", ComponentID: "pk-ui.component.modalpanel", Group: "Overlay", Name: "Modal / panel"},
			components.ModalProps{Title: "Panel only"}, components.ModalSlots{Body: []g.Node{g.Text("Body")}}, components.ModalPanelWithSlots),
		ExamplePreview(ExampleInfo{ID: "pk-ui.component.modal/form", ComponentID: "pk-ui.component.modalform", Group: "Overlay", Name: "Modal / form"},
			components.ModalForm(g.Text("Fields")), "ModalForm accepts arbitrary nodes rather than typed properties."),
		ExamplePreview(ExampleInfo{ID: "pk-ui.component.modal/close-button", ComponentID: "pk-ui.component.modalclosebutton", Group: "Overlay", Name: "Modal / close button"},
			components.ModalCloseButton("Close", ""), "ModalCloseButton accepts strings rather than a typed properties contract."),
		ExamplePreview(ExampleInfo{ID: "pk-ui.component.modal/cancel-button", ComponentID: "pk-ui.component.modalcancelbutton", Group: "Overlay", Name: "Modal / cancel button"},
			components.ModalCancelButton("Cancel", ""), "ModalCancelButton accepts strings rather than a typed properties contract."),
		ExampleOf(info("pk-ui.component.confirmdialog/default", "Overlay", "ConfirmDialog"), components.ConfirmDialogProps{
			Title: "Delete this row?", AcceptLabel: "Delete", CancelLabel: "Keep"}, components.ConfirmDialog),

		ExamplePreview(info("pk-ui.component.skiplink/default", "Frame", "SkipLink"), components.SkipLink("content", "Skip to content"), "SkipLink accepts strings rather than a typed properties contract."),
		ExampleWithChildren(info("pk-ui.component.toolbar/default", "Frame", "Toolbar"),
			components.ToolbarProps{Title: "Tasks", Subtitle: "12 records"}, []g.Node{
				ExampleWithSlots(ExampleInfo{ID: "action", ComponentID: "pk-ui.component.button"},
					components.ButtonProps{Label: "New task", Href: "/admin/task/tasks/new"}, components.ButtonSlots{}, components.ButtonWithSlots).Node,
			}, components.Toolbar),
		ExampleWithChildren(info("pk-ui.component.form/default", "Frame", "Form"),
			components.FormProps{Action: "/admin/task/tasks", Label: "New task"}, []g.Node{
				ExampleOf(ExampleInfo{ID: "title", ComponentID: "pk-ui.component.input"},
					components.InputProps{Name: "title", Label: "Title", Required: true}, components.Input).Node,
				ExampleWithChildren(ExampleInfo{ID: "actions", ComponentID: "pk-ui.component.formactions"}, components.FormActionsProps{}, []g.Node{
					ExampleWithSlots(ExampleInfo{ID: "cancel", ComponentID: "pk-ui.component.button"},
						components.ButtonProps{Label: "Cancel", Variant: "secondary", Href: "/admin/task/tasks"}, components.ButtonSlots{}, components.ButtonWithSlots).Node,
					ExampleWithSlots(ExampleInfo{ID: "create", ComponentID: "pk-ui.component.button"},
						components.ButtonProps{Label: "Create", Type: "submit"}, components.ButtonSlots{}, components.ButtonWithSlots).Node,
				}, components.FormActions).Node,
			}, components.Form),
		// Shell is not in this list, and cannot be: it renders <main>, and the
		// page this gallery is on is itself a Shell, so an example would be a
		// second main landmark inside the first — two documents in one, which
		// is exactly what the landmark rules exist to catch. The page is the
		// example. Its classes are covered by modules/admin's own closure test,
		// which renders every real screen and checks each class against the
		// stylesheet.
	}
}

// GalleryGroups is the groups the examples fall into, in order, so a page a
// hundred specimens long can be jumped through. Derived from the list above, so
// a new group needs no second edit.
func GalleryGroups() []string {
	var out []string
	for _, example := range Gallery() {
		if len(out) == 0 || out[len(out)-1] != example.Group {
			out = append(out, example.Group)
		}
	}
	return out
}

// Documentation is what a person needs in order to use the component beside it:
// the id the design export names it by, every property its Props type takes with
// the value this example gave it, and the slots something else can be put into.
//
// It is projected from the same Example the specimen is rendered from — the
// schema and the props are the ones export.Export publishes — so a page cannot
// describe a component this package does not have, or a property it does not
// take. That is the reason it exists: a wall of specimens says what a badge
// looks like and nothing about how to ask for one.
func Documentation(e Example) g.Node {
	described, err := e.Describe()
	if err != nil {
		return components.Text(components.TextProps{Content: "Cannot be described: " + err.Error(), Size: "sm", Color: "muted"})
	}
	facts := []g.Node{components.Text(components.TextProps{Content: described.ID, Element: "code", Size: "xs", Color: "muted"})}
	switch rows := propertyRows(described); {
	case !described.PropsEditable:
		facts = append(facts, components.Text(components.TextProps{Size: "sm", Color: "muted",
			Content: "No typed properties: " + cmp.Or(described.Reason, "captured as a rendered node")}))
	case len(rows) > 0:
		facts = append(facts, components.Table(components.TableProps{Compact: true, Rows: rows, Columns: []components.TableColumn{
			{Key: "name", Label: "Property", Primary: true},
			{Key: "type", Label: "Type"},
			{Key: "value", Label: "This example"},
			{Key: "choices", Label: "Allowed values"},
			{Key: "description", Label: "Description"},
		}}))
	}
	var named []string
	for _, slot := range described.Slots {
		// A slot the editors cannot fill is left out: naming it would be an
		// offer this package does not make.
		if slot.Supported {
			named = append(named, slot.Name)
		}
	}
	if len(named) > 0 {
		facts = append(facts, components.Text(components.TextProps{
			Content: "Slots: " + strings.Join(named, ", "), Size: "sm", Color: "muted"}))
	}
	return components.Stack(components.StackProps{Gap: "2"}, facts...)
}

// propertyRows is every property the component takes, the required ones first
// and then alphabetically, each with what this example gave it. A property left
// out is shown empty rather than omitted: what a component will accept is the
// half of this page a specimen cannot show. A value is written as a person
// would type it — a string without its quotes, anything else as JSON.
func propertyRows(d ExampleDescription) []components.TableRow {
	// A property that may be left unset is spelled anyOf[type, null] — a *bool
	// is how this package says "true, false, or the component's own default" —
	// so the type is read from either shape or the column would be blank for
	// exactly the properties whose absence means something.
	type propertyType struct {
		Type        any    `json:"type"`
		Enum        []any  `json:"enum"`
		Description string `json:"description"`
		AnyOf       []struct {
			Type string `json:"type"`
		} `json:"anyOf"`
	}
	var schema struct {
		Properties map[string]propertyType `json:"properties"`
		Required   []string                `json:"required"`
	}
	var given map[string]json.RawMessage
	if json.Unmarshal(d.Schema, &schema) != nil {
		return nil
	}
	_ = json.Unmarshal(d.Props, &given)
	names := slices.Sorted(maps.Keys(schema.Properties))
	rank := func(name string) int {
		if slices.Contains(schema.Required, name) {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(names, func(a, b string) int { return rank(a) - rank(b) })
	rows := make([]components.TableRow, 0, len(names))
	for _, name := range names {
		property := schema.Properties[name]
		kind, _ := property.Type.(string)
		if types, ok := property.Type.([]any); ok {
			var names []string
			for _, typ := range types {
				names = append(names, fmt.Sprint(typ))
			}
			kind = strings.Join(names, " or ")
		}
		for _, one := range property.AnyOf {
			if kind == "" && one.Type != "null" {
				kind = one.Type + ", or the default"
			}
		}
		if rank(name) == 0 {
			kind += ", required"
		}
		value := ""
		if raw := given[name]; len(raw) > 0 {
			if value = string(raw); json.Unmarshal(raw, &value) != nil {
				value = string(raw)
			}
		}
		var choices []string
		for _, choice := range property.Enum {
			text := fmt.Sprint(choice)
			if text == "" {
				text = "default"
			}
			choices = append(choices, text)
		}
		rows = append(rows, components.TableRow{ID: name, Cells: map[string]any{"name": name, "type": kind, "value": value, "choices": strings.Join(choices, ", "), "description": property.Description}})
	}
	return rows
}
