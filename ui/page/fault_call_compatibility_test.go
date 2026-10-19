package page_test

import (
	"net/http"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/ui/page"
)

func TestFaultKeepsItsPublishedCallSignatureBesideTheLocalizedForm(t *testing.T) {
	t.Parallel()
	var fault func(int, string, string, string) page.View = page.Fault
	portuguese := locale.SelectLocale(xtext.Load("en", page.Catalogue()), "pt-PT")
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			const detail = "<private> & 50% remaining"
			legacy := fault(status, detail, "/app?from=fault&next=home", "Return & continue")
			plain := page.FaultIn(nil, status, detail, "/app?from=fault&next=home", "Return & continue")
			localized := page.FaultIn(&portuguese, status, detail, "/app?from=fault&next=home", "Return & continue")
			if legacy.Status != status || legacy.Title != http.StatusText(status) {
				t.Fatalf("published call = status %d title %q", legacy.Status, legacy.Title)
			}
			source := render(t, g.Group(legacy.Body))
			if got := render(t, g.Group(plain.Body)); got != source {
				t.Errorf("nil locale changed the published rendering: %s", got)
			}
			if localized.Status != status || localized.Title == legacy.Title || localized.Title == "" {
				t.Errorf("localized call = status %d title %q", localized.Status, localized.Title)
			}
			for _, view := range []page.View{legacy, localized} {
				body := render(t, g.Group(view.Body))
				for _, want := range []string{"&lt;private&gt; &amp; 50% remaining", `href="/app?from=fault&amp;next=home"`, "Return &amp; continue"} {
					if !strings.Contains(body, want) {
						t.Errorf("refusal lost caller data %q: %s", want, body)
					}
				}
			}
		})
	}
}
