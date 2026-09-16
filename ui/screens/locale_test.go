package screens_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// perPage mirrors the generator's page size, which is the API's default limit.
const perPage = crud.DefaultLimit

// translations is a Formatter the way a composed catalog behaves: a known key
// is translated, an unknown one formats its source text.
type translations map[string]string

func (m translations) Text(key, fallback string, args ...any) string {
	if text, ok := m[key]; ok {
		return fmt.Sprintf(text, args...)
	}
	return fmt.Sprintf(fallback, args...)
}

// The generated screens speak the request's language through the same seam
// the sign-in page uses: page.Locale on the options, screens.* keys, and the
// authored English when no catalog is composed or a key is missing.
func TestScreensReadTheirLabelsThroughTheRequestLocale(t *testing.T) {
	t.Parallel()
	rows := []map[string]any{{"id": "1", "title": "Buy milk", "status": "open", "rank": 2.0, "pinned": true}}
	english := render(t, screens.List(resource(), opts, rows, 1, 1, "", true).Body)
	for _, want := range []string{"1 note", ">New note<"} {
		if !strings.Contains(english, want) {
			t.Fatalf("english list lacks %q:\n%s", want, english)
		}
	}
	// Three pages of rows, so the pager renders and its landmark is named.
	paged := render(t, screens.List(resource(), opts, rows, 3*perPage, 1, "", true).Body)
	if !strings.Contains(paged, fmt.Sprintf("%d notes", 3*perPage)) || !strings.Contains(paged, `aria-label="Pagination"`) {
		t.Fatalf("english paged list lacks its plural count or pager:\n%s", paged)
	}

	pt := opts
	pt.Locale = &page.Locale{Language: "pt-PT", Formatter: translations{
		"screens.count": "%d %s registados", "screens.new": "Nova %s", "screens.edit": "Editar",
		"screens.delete": "Eliminar", "screens.delete_this": "Eliminar esta %s", "screens.empty": "Ainda sem %ss.",
		"screens.pagination": "Paginação", "screens.breadcrumb": "Caminho",
	}}
	list := render(t, screens.List(resource(), pt, rows, 3*perPage, 1, "", true).Body)
	for _, want := range []string{fmt.Sprintf("%d notes registados", 3*perPage), ">Nova note<", `aria-label="Paginação"`} {
		if !strings.Contains(list, want) {
			t.Errorf("localized list lacks %q:\n%s", want, list)
		}
	}
	if !strings.Contains(render(t, screens.List(resource(), pt, nil, 0, 1, "", false).Body), "Ainda sem notes.") {
		t.Error("the empty state kept its English")
	}
	detail := render(t, screens.Detail(resource(), pt, rows[0], true).Body)
	for _, want := range []string{">Editar<", ">Eliminar<", `data-confirm-label="Eliminar"`, `aria-label="Eliminar esta note"`, `aria-label="Caminho"`} {
		if !strings.Contains(detail, want) {
			t.Errorf("localized detail lacks %q:\n%s", want, detail)
		}
	}
	// A key the catalog lacks keeps its readable English.
	if !strings.Contains(detail, "This deletes the note. It cannot be undone.") {
		t.Error("a missing key did not fall back to the authored text")
	}
	if strings.Contains(detail, ">Edit<") || strings.Contains(detail, ">Delete<") {
		t.Error("a translated action left the English behind")
	}
}
