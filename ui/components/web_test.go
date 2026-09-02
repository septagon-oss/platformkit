// Validates: REQ-011.

package components

// web_test.go closes the loop this package promises: every class a renderer
// can emit is declared in classlists.go, every declared class resolves to a
// CSS rule through tw/emission, and the rendered HTML carries the
// accessibility structure the contracts imply. The golden file makes markup
// changes reviewable diffs.

import (
	"bytes"
	"regexp"
	"sort"
	"strings"
	"testing"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/style"
)

// gallery is the package's own registry, rendered. See gallery.go: the tests
// and /admin/_gallery read one list, so a component the shell can show is a
// component whose classes the closure test below has seen.
func gallery() []g.Node {
	out := make([]g.Node, 0, len(Gallery()))
	for _, example := range Gallery() {
		out = append(out, example.Node)
	}
	return out
}

func TestIconRendersAccessibleEditableSVG(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	err := Icon(IconProps{
		Name:      "check",
		Size:      "lg",
		Tone:      "success",
		AriaLabel: "Complete",
	}).Render(&rendered)
	if err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, fragment := range []string{
		`<svg`,
		`viewBox="0 0 256 256"`,
		`fill="currentColor"`,
		`focusable="false"`,
		`data-pk-icon="check"`,
		`role="img"`,
		`aria-label="Complete"`,
		`<path`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("Icon output is missing %q: %s", fragment, html)
		}
	}
	if strings.Contains(html, "<span") || strings.Contains(html, "aria-hidden") {
		t.Errorf("labelled Icon has invalid wrapper or a11y state: %s", html)
	}
}

func TestIconUnknownNameUsesVisibleDecorativeFallback(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	if err := Icon(IconProps{Name: "not-a-real-icon"}).Render(&rendered); err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, fragment := range []string{
		`data-pk-icon="not-a-real-icon"`,
		`data-pk-icon-fallback="true"`,
		`aria-hidden="true"`,
		`<path`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("fallback Icon output is missing %q: %s", fragment, html)
		}
	}
}

func TestButtonOwnsNativeLinkSlotsAndStateContract(t *testing.T) {
	t.Parallel()

	var native strings.Builder
	if err := ButtonWithSlots(ButtonProps{
		ComponentProps: ComponentProps{
			ID:       "save",
			Disabled: true,
			Attrs:    map[string]string{"data-action": "save"},
		},
		HTMXProps: HTMXProps{Post: "/save", Target: "#result", Include: "#save-form"},
		Label:     "Saving",
		Variant:   "primary",
		Tone:      "success",
		Size:      "lg",
		Type:      "submit",
		Loading:   true,
	}, ButtonSlots{}).Render(&native); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`<button`, `id="save"`, `disabled`, `type="submit"`,
		`data-component="button"`, `data-variant="primary"`, `data-tone="success"`,
		`data-loading="true"`, `aria-busy="true"`, `hx-post="/save"`,
		`hx-target="#result"`, `hx-include="#save-form"`, `data-action="save"`, `Saving`,
		`text-fg-on-brand`, `border-t-fg-on-brand`,
	} {
		if !strings.Contains(native.String(), fragment) {
			t.Errorf("native Button output is missing %q: %s", fragment, native.String())
		}
	}
	if strings.Contains(native.String(), "border-t-fg-brand") {
		t.Errorf("loading Button retained an invisible brand-on-brand Spinner arc: %s", native.String())
	}

	secondary := renderNodeToString(t, Button(ButtonProps{
		Label: "Waiting", Variant: "secondary", Tone: "neutral", Loading: true,
	}))
	for _, fragment := range []string{"text-fg-primary", "border-t-fg-primary"} {
		if !strings.Contains(secondary, fragment) {
			t.Errorf("secondary loading Button did not preserve current foreground %q: %s", fragment, secondary)
		}
	}
	if strings.Contains(secondary, "border-t-fg-on-brand") {
		t.Errorf("secondary loading Button forced filled-action contrast: %s", secondary)
	}

	var link strings.Builder
	if err := ButtonWithSlots(ButtonProps{
		ComponentProps: ComponentProps{Disabled: true},
		Label:          "Continue",
		Href:           "/continue",
		AriaLabel:      "Continue to checkout",
	}, ButtonSlots{Content: []g.Node{h.Span(g.Text("Compound action"))}}).Render(&link); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`<a`, `href="/continue"`, `data-button-as-link="true"`,
		`aria-disabled="true"`, `tabindex="-1"`,
		`aria-label="Continue to checkout"`, `Compound action`,
	} {
		if !strings.Contains(link.String(), fragment) {
			t.Errorf("link Button output is missing %q: %s", fragment, link.String())
		}
	}
	for _, forbidden := range []string{` type=`, ` disabled="`, `>Continue<`} {
		if strings.Contains(link.String(), forbidden) {
			t.Errorf("link Button output unexpectedly contains %q: %s", forbidden, link.String())
		}
	}
}

func TestTextOwnsSemanticElementTypographyAndClampContract(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	if err := Text(TextProps{
		ComponentProps: ComponentProps{
			ID: "summary", Class: "product-copy", Attrs: map[string]string{"data-owner": "profile"},
		},
		Content: "Important account summary", Element: "STRONG", Size: "5xl",
		Align: "justify", Weight: "extra bold", Color: "tertiary",
		Transform: "uppercase", Italic: true, Underline: true, NoWrap: true,
		Truncate: true, Lines: 20,
	}).Render(&rendered); err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, fragment := range []string{
		`<strong`, `id="summary"`, `product-copy`, `data-owner="profile"`,
		`data-component="text"`, `data-element="strong"`, `data-size="5xl"`,
		`data-align="justify"`, `data-weight="extrabold"`, `data-color="tertiary"`,
		`text-5xl`, `text-justify`, `font-extrabold`, `text-fg-tertiary`,
		`uppercase`, `italic`, `underline`, `whitespace-nowrap`, `line-clamp-6`,
		`data-lines="6"`, `Important account summary`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("canonical Text missing %q: %s", fragment, html)
		}
	}
	if strings.Contains(html, ` truncate`) {
		t.Fatalf("line-clamped Text must not also use the single-line truncate contract: %s", html)
	}
}

func TestTextRejectsHeadingTagsAndNormalizesDefaults(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	if err := Text(TextProps{Content: "Body copy", Element: "h1", Color: "danger"}).Render(&rendered); err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, fragment := range []string{
		`<p`, `data-element="p"`, `data-size="base"`, `data-align="left"`,
		`data-weight="normal"`, `data-color="danger"`, `text-fg-danger`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("normalized Text missing %q: %s", fragment, html)
		}
	}
	if strings.Contains(html, `<h1`) {
		t.Fatalf("Text must not bypass the canonical Heading component: %s", html)
	}
}

func TestBreadcrumbOwnsTruncationAndIconRendering(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	err := Breadcrumb(BreadcrumbProps{
		ComponentProps: ComponentProps{
			ID:    "account-trail",
			Class: "client-breadcrumb",
			Attrs: map[string]string{"data-client": "collect"},
		},
		HTMXProps: HTMXProps{Boost: true},
		Separator: "›",
		MaxItems:  3,
		Items: []BreadcrumbItem{
			{Label: "Home", Href: "/", Icon: "home"},
			{Label: "Workspace", Href: "/workspace"},
			{Label: "Settings", Href: "/settings"},
			{Label: "Profile", Current: true},
		},
	}).Render(&rendered)
	if err != nil {
		t.Fatal(err)
	}

	html := rendered.String()
	for _, fragment := range []string{
		`id="account-trail"`,
		`class="client-breadcrumb"`,
		`data-client="collect"`,
		`data-component="breadcrumb"`,
		`hx-boost="true"`,
		`data-pk-icon="home"`,
		`href="/"`,
		`>…</li>`,
		`href="/settings"`,
		`aria-current="page">Profile`,
		`>›</li>`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("Breadcrumb output is missing %q: %s", fragment, html)
		}
	}
	if strings.Contains(html, "Workspace") {
		t.Errorf("Breadcrumb did not collapse its middle items: %s", html)
	}
}

func TestSidebarOwnsNestedActiveNavigationAndPortableBrand(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	err := Sidebar(SidebarProps{
		ComponentProps: ComponentProps{ID: "workspace", Class: "client-sidebar"},
		Current:        "/admin/customers/accounts",
		BrandLabel:     "Acme Control",
		BrandHref:      "/control",
		Sections: []SidebarSection{{
			ID: "operate", Label: "Operate", Glyph: "O", Tone: "brand",
			Items: []SidebarItem{
				{ID: "dashboard", Label: "Dashboard", Href: "/admin", Icon: "home"},
				{ID: "customers", Label: "Customers", Href: "/admin/customers", Icon: "users", Badge: "24", Children: []SidebarItem{
					{ID: "accounts", Label: "Accounts", Href: "/admin/customers/accounts"},
				}},
			},
		}},
	}).Render(&rendered)
	if err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, fragment := range []string{
		`<aside id="workspace"`, `client-sidebar`, `data-component="sidebar"`,
		`data-sidebar-flavor="admin"`, `aria-label="Admin navigation"`,
		`data-sidebar-brand=""`, `href="/control"`, `Acme Control`,
		`data-sidebar-section="operate"`, `data-sidebar-tone="brand"`,
		`text-fg-on-inverse`,
		`data-sidebar-item="customers"`, `data-sidebar-item="accounts"`,
		`data-active="true"`, `aria-current="page"`, `data-has-badge="true"`,
		`data-component="badge"`, `data-sidebar-submenu="customers"`,
		`data-pk-icon="users"`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("canonical Sidebar is missing %q: %s", fragment, html)
		}
	}
	if strings.Contains(html, `data-sidebar-item="dashboard" data-sidebar-depth="0" data-active="true"`) {
		t.Fatalf("the /admin root must not prefix-match every admin route: %s", html)
	}
}

func TestModalOwnsAccessibleOverlayAndServerBehaviorContract(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	err := ModalWithSlots(ModalProps{
		ComponentProps: ComponentProps{ID: "archive-modal", Class: "client-modal"},
		Title:          "Archive project", Description: "This cannot be undone.", Size: "large",
		CloseLabel: "Close archive dialog", Open: true, OpenOnSwap: true,
	}, ModalSlots{
		Body:   []g.Node{h.P(g.Text("Rich body"))},
		Footer: []g.Node{h.Button(g.Text("Archive"))},
	}).Render(&rendered)
	if err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, fragment := range []string{
		`id="archive-modal"`, `client-modal`, `data-component="modal"`,
		`data-controller="htmx-modal"`, `data-htmx-modal-open-value="true"`,
		`data-htmx-modal-close-on-escape-value="true"`, `data-htmx-modal-clear-on-close-value="false"`,
		`data-state="open"`, `role="dialog"`, `aria-modal="true"`, `aria-hidden="false"`,
		`aria-labelledby="archive-modal-title"`, `id="archive-modal-title"`,
		`data-action="htmx:afterSwap-&gt;htmx-modal#show"`, `data-modal-backdrop`,
		`data-action="click-&gt;htmx-modal#close"`, `data-modal-panel`, `tabindex="-1"`,
		`max-w-3xl`, `aria-label="Close archive dialog"`, `data-pk-icon="x"`,
		`data-modal-separator="header"`, `data-modal-body`, `Rich body`,
		`data-modal-separator="footer"`, `data-modal-footer`, `Archive`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("canonical Modal is missing %q: %s", fragment, html)
		}
	}
	headerSeparator := strings.Index(html, `data-modal-separator="header"`)
	body := strings.Index(html, `data-modal-body`)
	footerSeparator := strings.Index(html, `data-modal-separator="footer"`)
	footer := strings.Index(html, `data-modal-footer`)
	if headerSeparator < 0 || body < 0 || footerSeparator < 0 || footer < 0 ||
		!(headerSeparator < body && body < footerSeparator && footerSeparator < footer) {
		t.Errorf("Modal section boundaries are out of order: %s", html)
	}
}

func TestModalExplicitFalseOptionsRemoveDismissalAffordances(t *testing.T) {
	t.Parallel()

	no := false
	var rendered strings.Builder
	err := Modal(ModalProps{
		ComponentProps: ComponentProps{ID: "locked", Disabled: true},
		AriaLabel:      "Required decision", Size: "full", Body: "Choose one option.",
		Closable: &no, CloseOnOverlay: &no, CloseOnEscape: &no,
		ShowClose: &no, ShowOverlay: &no, Centered: &no,
	}).Render(&rendered)
	if err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, fragment := range []string{
		`aria-label="Required decision"`, `aria-disabled="true"`,
		`data-htmx-modal-open-value="false"`, `data-htmx-modal-close-on-escape-value="false"`,
		`data-state="closed"`, `hidden`, `style="display:none"`, `items-end`,
		`sm:items-center`, `max-w-full`, `tabindex="-1"`, `Choose one option.`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("non-dismissible Modal is missing %q: %s", fragment, html)
		}
	}
	for _, fragment := range []string{`data-modal-backdrop`, `data-modal-close`, `click-&gt;htmx-modal#close`} {
		if strings.Contains(html, fragment) {
			t.Errorf("non-dismissible Modal must not expose %q: %s", fragment, html)
		}
	}
}

func TestModalDeferredRootAndPanelFragmentsShareOneControllerContract(t *testing.T) {
	t.Parallel()

	var rootRendered strings.Builder
	err := Modal(ModalProps{
		ComponentProps: ComponentProps{ID: "entity-form-modal"},
		AriaLabel:      "Entity form", Deferred: true, OpenOnSwap: true,
	}).Render(&rootRendered)
	if err != nil {
		t.Fatal(err)
	}
	rootHTML := rootRendered.String()
	for _, fragment := range []string{
		`id="entity-form-modal"`, `data-controller="htmx-modal"`,
		`data-htmx-modal-clear-on-close-value="true"`,
		`data-action="htmx:afterSwap-&gt;htmx-modal#show click-&gt;htmx-modal#backdropClick"`,
		`hidden`, `style="display:none"`,
	} {
		if !strings.Contains(rootHTML, fragment) {
			t.Errorf("deferred Modal root is missing %q: %s", fragment, rootHTML)
		}
	}
	for _, fragment := range []string{`data-modal-panel`, `role="dialog"`} {
		if strings.Contains(rootHTML, fragment) {
			t.Errorf("deferred Modal root must initially be empty of %q: %s", fragment, rootHTML)
		}
	}

	var panelRendered strings.Builder
	err = ModalPanel(ModalProps{Title: "Edit entity"}, h.P(g.Text("Form body"))).Render(&panelRendered)
	if err != nil {
		t.Fatal(err)
	}
	panelHTML := panelRendered.String()
	for _, fragment := range []string{
		`data-modal-panel`, `role="dialog"`, `aria-modal="true"`,
		`aria-label="Edit entity"`, `click-&gt;htmx-modal#stopPropagation`, `Form body`,
	} {
		if !strings.Contains(panelHTML, fragment) {
			t.Errorf("server Modal panel is missing %q: %s", fragment, panelHTML)
		}
	}
}

func TestModalActionHelpersUseControllerWithoutInlineJavaScript(t *testing.T) {
	t.Parallel()

	for name, node := range map[string]g.Node{
		"close":  ModalCloseButton("Dismiss", "client-close"),
		"cancel": ModalCancelButton("Go back", "client-cancel"),
		"form":   ModalForm(h.ID("edit-form")),
	} {
		var rendered strings.Builder
		if err := node.Render(&rendered); err != nil {
			t.Fatalf("render %s helper: %v", name, err)
		}
		html := rendered.String()
		if strings.Contains(html, `onclick=`) {
			t.Errorf("%s helper must avoid inline JavaScript: %s", name, html)
		}
		if !strings.Contains(html, `htmx-modal#`) {
			t.Errorf("%s helper must use the canonical controller: %s", name, html)
		}
	}
}

func TestSidebarContentSlotsSearchHooksAndSafeItemData(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	err := SidebarWithSlots(SidebarProps{
		ComponentProps: ComponentProps{
			ID:    "docs",
			Attrs: map[string]string{"data-docs-search-root": "sidebar"},
		},
		Flavor:          "content",
		Current:         "#figma",
		NavigationLabel: "Docs navigation",
		Sections: []SidebarSection{{
			Label: "Runtime", Glyph: "R", Tone: "info", SearchText: "runtime architecture",
			Items: []SidebarItem{
				{Label: "Overview", Href: "#overview", Prefix: "01"},
				{Label: "Figma handoff", Href: "#figma", Prefix: "02", SearchText: "figma", Icon: `bad<script`},
			},
		}},
	}, SidebarSlots{
		Brand:  []g.Node{h.Strong(g.Text("Architecture decisions"))},
		Footer: []g.Node{h.Small(g.Text("Updated today"))},
	}).Render(&rendered)
	if err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, fragment := range []string{
		`id="docs"`, `data-sidebar-flavor="content"`, `aria-label="Docs navigation"`,
		`data-docs-search-root="sidebar"`, `<strong>Architecture decisions</strong>`,
		`data-sidebar-section="runtime"`, `data-sidebar-search-section="true"`,
		`data-sidebar-search-text="runtime architecture"`,
		`data-sidebar-search-item="true"`, `data-sidebar-item-prefix="true"`,
		`data-sidebar-item-label="true"`, `data-active="true"`,
		`data-sidebar-footer=""`, `<small>Updated today</small>`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("content Sidebar is missing %q: %s", fragment, html)
		}
	}
	if strings.Contains(html, "bad<script") {
		t.Fatalf("invalid icon names must not become markup: %s", html)
	}
}

func TestSidebarCollapsedAndDisabledStatesRemainAccessible(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	err := Sidebar(SidebarProps{
		ComponentProps: ComponentProps{Disabled: true},
		Collapsible:    true,
		Collapsed:      true,
		Items: []SidebarItem{{
			Label: "Reports", Href: "/admin/reports", Icon: "chart",
		}},
	}).Render(&rendered)
	if err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, fragment := range []string{
		`id="admin-sidebar-collapsible"`, `data-sidebar-collapsible="true"`,
		`data-sidebar-collapsed="true"`, `data-state="collapsed"`,
		`aria-expanded="false"`, `aria-disabled="true"`,
		`aria-label="Reports"`, `data-disabled="true"`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("collapsed Sidebar is missing %q: %s", fragment, html)
		}
	}
	if strings.Contains(html, `href="/admin/reports"`) {
		t.Fatalf("disabled Sidebar must not emit operable links: %s", html)
	}
}

func TestBreadcrumbOmitsEmptyNavigationLandmark(t *testing.T) {
	t.Parallel()

	if node := Breadcrumb(BreadcrumbProps{}); node != nil {
		t.Fatalf("empty breadcrumb = %#v, want nil", node)
	}
}

func TestCardOwnsSectionsMediaAndSurfaceVariants(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	err := CardWithSlots(CardProps{
		ComponentProps: ComponentProps{
			ID:    "account-card",
			Class: "client-card",
			Attrs: map[string]string{"data-client": "collect"},
		},
		Image:         "/avatar.webp",
		ImageAlt:      "Account owner",
		ImagePosition: "left",
		Variant:       "plain",
		Padding:       "small",
		Shadow:        "none",
		Hoverable:     true,
	}, CardSlots{
		Header:  []g.Node{h.H2(g.Text("Account"))},
		Content: []g.Node{h.P(g.Text("Current plan"))},
		Footer:  []g.Node{h.Button(g.Text("Manage"))},
	}).Render(&rendered)
	if err != nil {
		t.Fatal(err)
	}

	html := rendered.String()
	for _, fragment := range []string{
		`id="account-card"`,
		`class="`,
		`client-card`,
		`data-client="collect"`,
		`data-component="card"`,
		`src="/avatar.webp"`,
		`alt="Account owner"`,
		`>Account<`,
		`>Current plan<`,
		`>Manage<`,
		clCardPadSmall.Compile(),
		clCardHeader.Compile(),
		clCardFooter.Compile(),
		clCardHoverable.Compile(),
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("CardWithSlots output is missing %q: %s", fragment, html)
		}
	}
	for _, fragment := range []string{clCardBorder.Compile(), clCardShadowSmall.Compile()} {
		if strings.Contains(html, fragment) {
			t.Errorf("plain shadowless CardWithSlots unexpectedly contains %q: %s", fragment, html)
		}
	}
}

func TestDividerPreservesLabelAndTrustedBaseAttributes(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	err := Divider(DividerProps{
		ComponentProps: ComponentProps{
			ID:    "auth-divider",
			Class: "my-divider",
			Attrs: map[string]string{"data-auth-divider": "true"},
		},
		Text: "Or continue with",
	}).Render(&rendered)
	if err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, fragment := range []string{
		`id="auth-divider"`,
		`class="flex items-center gap-4 my-divider"`,
		`data-auth-divider="true"`,
		`role="presentation"`,
		`aria-hidden="true"`,
		`Or continue with`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("labelled Divider output is missing %q: %s", fragment, html)
		}
	}
}

func TestSpinnerOwnsAccessibleStatusAndLabelDefault(t *testing.T) {
	t.Parallel()

	var labelled strings.Builder
	if err := Spinner(SpinnerProps{Label: "Fetching results", Size: "lg", Tone: "info"}).Render(&labelled); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`role="status"`, `aria-label="Fetching results"`} {
		if !strings.Contains(labelled.String(), fragment) {
			t.Errorf("canonical Spinner missing %s: %s", fragment, labelled.String())
		}
	}

	var defaultLabel strings.Builder
	if err := Spinner(SpinnerProps{}).Render(&defaultLabel); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(defaultLabel.String(), `aria-label="Loading"`) {
		t.Fatalf("canonical Spinner lost its accessible default label: %s", defaultLabel.String())
	}
}

func TestTextareaOwnsValidationCounterAndAutoresizeContract(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	err := Textarea(TextareaProps{
		ComponentProps: ComponentProps{
			Disabled: true,
			Attrs:    map[string]string{"data-field-owner": "profile"},
		},
		HTMXProps: HTMXProps{Post: "/notes", Trigger: "blur"},
		Name:      "notes", Label: "Notes", Value: "hello", Placeholder: "Add notes",
		HelperText: "Visible to administrators.", ErrorMessage: "Add more detail.",
		Required: true, ReadOnly: true, MinLength: 2, MaxLength: 20,
		AutoResize: true, MinRows: 3, MaxRows: 15, FullWidth: true,
	}).Render(&rendered)
	if err != nil {
		t.Fatal(err)
	}

	html := rendered.String()
	for _, fragment := range []string{
		`data-component="textarea"`,
		`data-controller="textarea-counter"`,
		`data-controller="autoresize"`,
		`data-autoresize-min-rows-value="3"`,
		`data-autoresize-max-rows-value="15"`,
		`data-textarea-counter-target="input"`,
		`data-textarea-counter-target="display"`,
		`data-action="input-&gt;autoresize#resize input-&gt;textarea-counter#update"`,
		`aria-describedby="pk-textarea-notes-error pk-textarea-notes-helper"`,
		`id="pk-textarea-notes-error"`,
		`id="pk-textarea-notes-helper"`,
		`role="alert"`,
		`aria-live="polite"`,
		`5 / 20`,
		`rows="3"`,
		`minlength="2"`,
		`maxlength="20"`,
		`required`,
		`readonly`,
		`disabled`,
		`hx-post="/notes"`,
		`hx-trigger="blur"`,
		`data-field-owner="profile"`,
		`resize-none`,
		`w-full`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("canonical Textarea missing %s: %s", fragment, html)
		}
	}
	if !strings.Contains(html, "Add more detail.") || !strings.Contains(html, "Visible to administrators.") {
		t.Errorf("canonical Textarea must render error and helper together: %s", html)
	}
}

func TestTextareaDefaultsToManualResizeAndAccessibleName(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	if err := Textarea(TextareaProps{Name: "summary"}).Render(&rendered); err != nil {
		t.Fatal(err)
	}
	html := rendered.String()
	for _, fragment := range []string{`rows="4"`, `resize-y`, `aria-label="summary"`} {
		if !strings.Contains(html, fragment) {
			t.Errorf("default Textarea missing %s: %s", fragment, html)
		}
	}
	if strings.Contains(html, `data-controller="autoresize"`) || strings.Contains(html, `textarea-counter`) {
		t.Errorf("optional Textarea controllers must be state-gated: %s", html)
	}
}

func TestTableWithSlotsOwnsRichCellsAndServerSorting(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	err := TableWithSlots(TableProps{
		HTMXProps: HTMXProps{
			Target: "#users-grid",
			Swap:   "outerHTML",
		},
		Sortable:   true,
		Selectable: true,
		Columns: []TableColumn{
			{Key: "name", Label: "Name", Sortable: true, Width: "12rem"},
			{Key: "actions", Label: "Actions"},
		},
		Rows: []TableRow{{
			ID: "user-1",
			Cells: map[string]any{
				"name":    "Ada",
				"actions": "plain fallback",
			},
		}},
	}, TableSlots{
		Cell: func(row TableRow, column TableColumn) g.Node {
			if column.Key == "actions" {
				return g.El("strong", g.Text("Rich action"))
			}
			return nil
		},
		SortURL: func(column TableColumn) string {
			return "/users?sort=" + column.Key
		},
		SortState: func(column TableColumn) string {
			return "ascending"
		},
		SelectAllLabel: "Select every user",
		SelectRowLabel: func(row TableRow) string {
			return "Select " + row.ID
		},
	}).Render(&rendered)
	if err != nil {
		t.Fatal(err)
	}

	html := rendered.String()
	for _, fragment := range []string{
		`data-component="table"`,
		`aria-label="Select every user"`,
		`aria-label="Select user-1"`,
		`aria-sort="ascending"`,
		`style="width:12rem"`,
		`hx-get="/users?sort=name"`,
		`hx-target="#users-grid"`,
		`hx-swap="outerHTML"`,
		`<strong>Rich action</strong>`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("TableWithSlots output is missing %q: %s", fragment, html)
		}
	}
	if strings.Contains(html, "plain fallback") {
		t.Errorf("rich cell slot must replace the fallback value: %s", html)
	}
}

func TestTableWithSlotsCarriesTrustedRowAndCellProjection(t *testing.T) {
	var buf bytes.Buffer
	err := TableWithSlots(TableProps{
		Selectable: true,
		Sortable:   true,
		Columns:    []TableColumn{{Key: "name", Label: "Name", Sortable: true}},
		Rows:       []TableRow{{ID: "row-1", Cells: map[string]any{"name": "Ada"}}},
	}, TableSlots{
		RowAttrs: func(TableRow) []g.Node { return []g.Node{g.Attr("data-state", "selected")} },
		CellAttrs: func(_ TableRow, c TableColumn) []g.Node {
			return []g.Node{g.Attr("data-label", c.Label)}
		},
		SortButtonAttrs:  func(TableColumn) []g.Node { return []g.Node{g.Attr("hx-include", "[name='search']")} },
		SelectRowChecked: func(TableRow) bool { return true },
	}).Render(&buf)
	if err != nil {
		t.Fatalf("render table slots: %v", err)
	}
	for _, fragment := range []string{
		`data-state="selected"`, `data-label="Name"`, `hx-include="[name=&#39;search&#39;]"`, `checked`,
	} {
		if !strings.Contains(buf.String(), fragment) {
			t.Errorf("TableWithSlots output is missing %q: %s", fragment, buf.String())
		}
	}
}

func TestSelectOwnsGroupedMultipleAndSupportingTextContract(t *testing.T) {
	var output strings.Builder
	err := Select(SelectProps{
		ComponentProps: ComponentProps{ID: "team-filter"},
		Name:           "teams",
		Values:         []string{"platform", "design"},
		Multiple:       true,
		FullWidth:      true,
		Error:          "Choose at least one team.",
		HelpText:       "Use Shift to select a range.",
		Options: []SelectOption{
			{Value: "platform", Label: "Platform", Group: "Engineering", Description: "Core platform team"},
			{Value: "design", Label: "Design", Group: "Engineering"},
			{Value: "operations", Label: "Operations", Group: "Business", Disabled: true},
		},
	}).Render(&output)
	if err != nil {
		t.Fatalf("render Select: %v", err)
	}

	html := output.String()
	for _, fragment := range []string{
		`aria-label="teams"`,
		`aria-describedby="team-filter-error team-filter-help"`,
		`multiple`,
		`size="4"`,
		`optgroup label="Engineering"`,
		`value="platform" selected`,
		`title="Core platform team"`,
		`value="operations" disabled`,
		`role="alert"`,
		`Choose at least one team.`,
		`Use Shift to select a range.`,
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("Select output missing %q: %s", fragment, html)
		}
	}
}

func TestInputOwnsValidationToneConstraintsAndSupportingText(t *testing.T) {
	var output strings.Builder
	err := Input(InputProps{
		ComponentProps: ComponentProps{ID: "email-field"},
		Name:           "email",
		Type:           "EMAIL",
		Value:          "name@example.com",
		ReadOnly:       true,
		FullWidth:      true,
		Tone:           "warning",
		Error:          "Review this address.",
		HelpText:       "Use your work email.",
		MinLength:      5,
		MaxLength:      120,
		Pattern:        `.+@.+`,
		Autocomplete:   "email",
	}).Render(&output)
	if err != nil {
		t.Fatalf("render Input: %v", err)
	}

	html := output.String()
	for _, fragment := range []string{
		`id="email-field"`,
		`name="email"`,
		`type="email"`,
		`data-tone="warning"`,
		`data-size="md"`,
		`readonly`,
		`minlength="5"`,
		`maxlength="120"`,
		`pattern=".+@.+"`,
		`autocomplete="email"`,
		`aria-label="email"`,
		`aria-invalid="true"`,
		`aria-describedby="email-field-error email-field-help"`,
		`role="alert"`,
		`Review this address.`,
		`Use your work email.`,
		`text-fg-secondary`,
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("Input output missing %q: %s", fragment, html)
		}
	}

	ordinary := renderNodeToString(t, Input(InputProps{
		Name:  "display-name",
		Value: "Joao",
	}))
	if !strings.Contains(ordinary, "text-fg-primary") {
		t.Errorf("ordinary Input value lost primary text color: %s", ordinary)
	}
	if strings.Contains(ordinary, "text-fg-secondary") {
		t.Errorf("ordinary Input value inherited read-only text color: %s", ordinary)
	}
}

func renderAll(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, n := range gallery() {
		if n == nil {
			continue
		}
		if err := n.Render(&b); err != nil {
			t.Fatalf("render: %v", err)
		}
		b.WriteString("\n")
	}
	return b.String()
}

var classAttrRE = regexp.MustCompile(`class="([^"]*)"`)

// TestRenderedClassesAreDeclared is the architectural loop-closure: every
// class in rendered HTML must be covered by the stylesheet derived from
// ClassLists(). A renderer inventing a class the lists do not declare, or a
// list drifting from a renderer, fails here — which is exactly the failure
// Tailwind users hit at runtime as silently unstyled markup.
func TestRenderedClassesAreDeclared(t *testing.T) {
	t.Parallel()

	sheet, err := style.For(ClassLists()...)
	if err != nil {
		t.Fatalf("style.For over declared lists: %v", err)
	}
	css, err := sheet.CSS(), error(nil)
	if err != nil {
		t.Fatal(err)
	}

	html := renderAll(t)
	seen := map[string]bool{}
	for _, m := range classAttrRE.FindAllStringSubmatch(html, -1) {
		for _, class := range strings.Fields(m[1]) {
			seen[class] = true
		}
	}
	if len(seen) < 60 {
		t.Fatalf("only %d distinct classes rendered; the gallery regressed", len(seen))
	}

	var missing []string
	for class := range seen {
		// HTMX owns this runtime sentinel and its visibility rule; it is not a
		// visual utility and therefore deliberately does not enter tw style.
		if class == "htmx-indicator" {
			continue
		}
		if _, err := style.Rules(class); err != nil {
			missing = append(missing, class+" (unresolvable)")
			continue
		}
		// The class must be in the derived sheet, not merely resolvable —
		// prefix-escaped for selector matching.
		esc := strings.NewReplacer(":", "\\:", "/", "\\/", "[", "\\[", "]", "\\]", ".", "\\.").Replace(class)
		if !strings.Contains(css, "."+esc) {
			missing = append(missing, class+" (not in For(ClassLists()) sheet)")
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("rendered class not backed by the design system: %s", m)
	}
}

func TestAccessibilityStructure(t *testing.T) {
	t.Parallel()
	html := renderAll(t)
	for _, want := range []string{
		`aria-invalid="true"`,                    // errored input
		`aria-describedby="pk-input-slug-error"`, // error linkage
		`for="pk-input-email"`,                   // label association
		`role="alert"`,                           // severe alert interrupts
		`role="status"`,                          // polite alert does not
		`aria-current="page"`,                    // breadcrumb + pagination current
		`aria-label="Breadcrumb"`,                // named navigation landmarks
		`aria-label="Pagination"`,
		`aria-selected="true"`,      // active tab
		`aria-hidden="true"`,        // decorative icons hidden
		`rel="noopener noreferrer"`, // external link safety
		`aria-busy="true"`,          // loading button
		`scope="col"`,               // table headers
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered gallery missing accessibility structure %s", want)
		}
	}
}

func TestAlertOwnsCanonicalInteractiveAndVisualStates(t *testing.T) {
	t.Parallel()

	node := Alert(AlertProps{
		Title:       "Check required",
		Message:     "Review this change.",
		Tone:        "warning",
		Dismissible: true,
		Bordered:    true,
		Compact:     true,
	})
	var output strings.Builder
	if err := node.Render(&output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, fragment := range []string{
		`role="alert"`,
		`aria-live="assertive"`,
		`data-controller="alert"`,
		`data-alert-dismissible-value="true"`,
		`data-alert-icon=""`,
		`data-pk-icon="exclamation-triangle"`,
		`data-action="click-&gt;alert#dismiss"`,
		`data-alert-close=""`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("canonical Alert missing %s: %s", fragment, html)
		}
	}
	for _, classList := range []string{
		clAlertCompact.Compile(),
		clAlertBordered.Compile(),
		clAlertIcon.Compile(),
		clAlertClose.Compile(),
	} {
		for _, class := range strings.Fields(classList) {
			if !strings.Contains(" "+html+" ", class) {
				t.Errorf("canonical Alert missing state class %q: %s", class, html)
			}
		}
	}
	for _, class := range strings.Fields(clAlertRegular.Compile()) {
		if strings.Contains(" "+html+" ", class) {
			t.Errorf("compact Alert retained regular-spacing class %q: %s", class, html)
		}
	}
}

func TestBadgeOwnsCountRemovalLiveAndSlotContract(t *testing.T) {
	t.Parallel()

	node := BadgeWithSlots(
		BadgeProps{
			ComponentProps: ComponentProps{ID: "messages", Class: "custom-badge"},
			Label:          "Messages",
			Variant:        "secondary",
			Tone:           "success",
			Size:           "sm",
			Dot:            true,
			Count:          125,
			Removable:      true,
			RemoveLabel:    "Clear message filter",
			Live:           true,
		},
		BadgeSlots{IconStart: []g.Node{Icon(IconProps{Name: "envelope", Size: "xs"})}},
	)
	var output strings.Builder
	if err := node.Render(&output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, fragment := range []string{
		`id="messages"`, `custom-badge`, `data-component="badge"`,
		`data-variant="secondary"`, `data-tone="success"`, `data-size="sm"`,
		`role="status"`, `aria-live="polite"`, `data-badge-dot="true"`,
		`data-pk-icon="envelope"`, `data-badge-count="true">99+`,
		`data-badge-remove="true"`, `aria-label="Clear message filter"`,
		`data-pk-icon="x-mark"`, `pl-1`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("canonical Badge missing %s: %s", fragment, html)
		}
	}
	dot := strings.Index(html, `data-badge-dot="true"`)
	leading := strings.Index(html, `data-pk-icon="envelope"`)
	label := strings.Index(html, `Messages`)
	count := strings.Index(html, `data-badge-count="true"`)
	remove := strings.Index(html, `data-badge-remove="true"`)
	if dot < 0 || leading < 0 || label < 0 || count < 0 || remove < 0 ||
		!(dot < leading && leading < label && label < count && count < remove) {
		t.Errorf("Badge child order diverges from its delivery blueprint: %s", html)
	}
}

func TestTabsOwnCanonicalPanelsAndRejectIconMarkup(t *testing.T) {
	t.Parallel()

	node := TabsWithPanels(
		TabsProps{ActiveTab: "disabled", Variant: "underline"},
		TabSlot{ID: "disabled", Label: "Disabled", Disabled: true},
		TabSlot{ID: "safe", Label: "Safe", Icon: `<img src=x onerror=alert(1)>`, Content: []g.Node{g.Text("Panel")}},
		TabSlot{ID: "lazy", Label: "Lazy", HxGet: "/lazy"},
	)
	var output strings.Builder
	if err := node.Render(&output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, fragment := range []string{
		`data-controller="tabs"`,
		`data-tabs-contract="1"`,
		`data-tabs-active-tab-value="safe"`,
		`data-action="click-&gt;tabs#activate"`,
		`role="tabpanel"`,
		`hx-trigger="tabs:activate from:this once"`,
		`aria-disabled="true"`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("canonical Tabs missing %s: %s", fragment, html)
		}
	}
	for _, unsafe := range []string{"<img", "onerror=", "<script"} {
		if strings.Contains(html, unsafe) {
			t.Errorf("tab icon name rendered unsafe markup %q: %s", unsafe, html)
		}
	}
}

func TestCheckboxOwnsIndeterminateControllerProjection(t *testing.T) {
	t.Parallel()

	node := Checkbox(CheckboxProps{
		ComponentProps: ComponentProps{ID: "select-some", Class: "w-fit"},
		Name:           "selection",
		Label:          "Select some rows",
		Indeterminate:  true,
		HelpText:       "Some rows are selected.",
	})
	var output strings.Builder
	if err := node.Render(&output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, fragment := range []string{
		`class="flex flex-col gap-1.5"`,
		`class="inline-flex items-start gap-3 cursor-pointer w-fit"`,
		`data-controller="checkbox"`,
		`data-checkbox-indeterminate-value="true"`,
		`data-state="indeterminate"`,
		`id="select-some"`,
		`name="selection"`,
		`aria-checked="mixed"`,
		`data-checkbox-input="true"`,
		`data-checkbox-box="true"`,
		`data-checkbox-checkmark="true"`,
		`data-checkbox-bar="true"`,
		`aria-describedby="select-some-help"`,
		`id="select-some-help"`,
	} {
		if !strings.Contains(html, fragment) {
			t.Errorf("canonical Checkbox missing %s: %s", fragment, html)
		}
	}
}

func TestPaginationOffsetPreservesQueryAndUsesPerPageHTMXURLs(t *testing.T) {
	t.Parallel()
	node := Pagination(PaginationProps{
		HTMXProps: HTMXProps{
			Get:    "/results?page_size=20&sort=name",
			Target: "#results",
			Swap:   "outerHTML",
		},
		CurrentPage: 2,
		TotalPages:  3,
		BaseURL:     "/results?page_size=20&sort=name",
	})
	var output strings.Builder
	if err := node.Render(&output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, fragment := range []string{
		`href="/results?page=1&amp;page_size=20&amp;sort=name"`,
		`hx-get="/results?page=1&amp;page_size=20&amp;sort=name"`,
		`href="/results?page=3&amp;page_size=20&amp;sort=name"`,
		`hx-get="/results?page=3&amp;page_size=20&amp;sort=name"`,
		`hx-target="#results"`,
		`hx-swap="outerHTML"`,
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("offset pagination missing %q: %s", fragment, html)
		}
	}
	if strings.Contains(html, "??") {
		t.Fatalf("offset pagination emitted a malformed URL: %s", html)
	}
}

func TestDetailListRendersExplicitAccessibleSectionSemantics(t *testing.T) {
	t.Parallel()

	node := DetailList(DetailListProps{
		ComponentProps: ComponentProps{ID: "profile-details"},
		Title:          "Perfil",
		Description:    "Dados usados nesta conta.",
		SemanticRole:   "identity",
		Items: []DetailItem{
			{Label: "Email", Value: "ada@example.test"},
			{Label: "Plano", Value: "Studio", Description: "Renova amanhã.", Tone: "brand"},
			{Label: "", Value: "must not render"},
		},
	})
	var output strings.Builder
	if err := node.Render(&output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, fragment := range []string{
		`data-component="detail-list"`,
		`data-semantic-role="identity"`,
		`aria-labelledby="profile-details-title"`,
		`aria-describedby="profile-details-description"`,
		`id="profile-details-title"`,
		`id="profile-details-description"`,
		`<dl`,
		`<dt`,
		`<dd`,
		`Perfil`,
		`Dados usados nesta conta.`,
		`Renova amanhã.`,
		`text-fg-brand`,
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("semantic detail list missing %q: %s", fragment, html)
		}
	}
	if strings.Contains(html, "must not render") || strings.Count(html, `data-detail-item`) != 2 {
		t.Fatalf("detail list did not fail closed for malformed facts: %s", html)
	}
}

func TestDetailListRejectsDisplayCopyAsSemanticRole(t *testing.T) {
	t.Parallel()

	node := DetailList(DetailListProps{
		SemanticRole: "Profile Details",
		Items:        []DetailItem{{Label: "Status", Value: "Ready"}},
	})
	var output strings.Builder
	if err := node.Render(&output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	if strings.Contains(html, "data-semantic-role") || strings.Contains(html, `role="Profile Details"`) {
		t.Fatalf("detail list projected invalid semantic role: %s", html)
	}
}

func renderNodeToString(t *testing.T, node g.Node) string {
	t.Helper()
	var output strings.Builder
	if err := node.Render(&output); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestNeutralToneRendersAcrossFeedbackAtoms(t *testing.T) {
	t.Parallel()

	spinner := renderNodeToString(t, Spinner(SpinnerProps{Tone: "neutral"}))
	if !strings.Contains(spinner, "border-t-fg-secondary") {
		t.Errorf("neutral Spinner missing neutral border tone: %s", spinner)
	}

	alert := renderNodeToString(t, Alert(AlertProps{Message: "Note", Tone: "neutral"}))
	for _, fragment := range []string{`data-alert-tone="neutral"`, `role="status"`, "bg-surface-tertiary"} {
		if !strings.Contains(alert, fragment) {
			t.Errorf("neutral Alert missing %s: %s", fragment, alert)
		}
	}
}
