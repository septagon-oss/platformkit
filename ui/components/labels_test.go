package components_test

import (
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	c "github.com/septagon-oss/platformkit/ui/components"
)

func html(t *testing.T, node g.Node) string {
	t.Helper()
	var b strings.Builder
	if err := node.Render(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// The labels a screen reader hears are Props with the authored English as
// their default, so an unlocalized shell renders exactly as before and a
// localized one supplies its own words through the same typed contract.
func TestNavigationLabelsDefaultToEnglishAndFollowProps(t *testing.T) {
	t.Parallel()
	pager := c.PaginationProps{CurrentPage: 2, TotalPages: 5, BaseURL: "/notes"}
	out := html(t, c.Pagination(pager))
	for _, want := range []string{`aria-label="Pagination"`, `aria-label="Previous page"`, `aria-label="Next page"`, `aria-label="Go to page 3"`, `aria-label="Page 2, current page"`} {
		if !strings.Contains(out, want) {
			t.Errorf("default pagination lacks %s:\n%s", want, out)
		}
	}
	pager.NavigationLabel, pager.PreviousLabel, pager.NextLabel = "Paginação", "Página anterior", "Página seguinte"
	pager.PageLabel, pager.CurrentPageLabel = "Ir para a página %d", "Página %d, atual"
	out = html(t, c.Pagination(pager))
	for _, want := range []string{`aria-label="Paginação"`, `aria-label="Página anterior"`, `aria-label="Página seguinte"`, `aria-label="Ir para a página 3"`, `aria-label="Página 2, atual"`} {
		if !strings.Contains(out, want) {
			t.Errorf("localized pagination lacks %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Previous page") || strings.Contains(out, "Go to page") {
		t.Error("a supplied label left the English behind")
	}
	// A label without the number marker renders as written: Props are data.
	pager.PageLabel = "Página"
	if !strings.Contains(html(t, c.Pagination(pager)), `aria-label="Página"`) {
		t.Error("a marker-less page label was not rendered verbatim")
	}

	crumbs := c.BreadcrumbProps{Items: []c.BreadcrumbItem{{Label: "Home", Href: "/"}, {Label: "Here"}}}
	if !strings.Contains(html(t, c.Breadcrumb(crumbs)), `aria-label="Breadcrumb"`) {
		t.Error("the breadcrumb landmark lost its default name")
	}
	crumbs.NavigationLabel = "Caminho"
	if !strings.Contains(html(t, c.Breadcrumb(crumbs)), `aria-label="Caminho"`) {
		t.Error("the breadcrumb landmark ignored its label")
	}

	alert := c.AlertProps{Message: "Saved", Dismissible: true}
	if !strings.Contains(html(t, c.Alert(alert)), `aria-label="Dismiss notification"`) {
		t.Error("the dismiss control lost its default name")
	}
	alert.DismissLabel = "Fechar aviso"
	if !strings.Contains(html(t, c.Alert(alert)), `aria-label="Fechar aviso"`) {
		t.Error("the dismiss control ignored its label")
	}
}
