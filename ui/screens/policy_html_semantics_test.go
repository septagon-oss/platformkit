package screens_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/ui/document"
	core "github.com/septagon-oss/platformkit/ui/resource"
	"golang.org/x/net/html"
	g "maragu.dev/gomponents"
)

func checkHTMLProbe(t *testing.T, r core.Resource, want webExpectation) {
	t.Helper()
	o := opts
	o.ComposedSource = "/composed/{reference}"
	record := webDOM(t, core.Detail(r, o, want.Probe, false))
	if want.EmptyTitle != "" {
		equalHTML(t, "empty title", webTexts(record, "h1"), []string{want.EmptyTitle})
	}
	for name, text := range want.Readings {
		i := slices.IndexFunc(r.Resolved.Fields, func(f present.ResolvedField) bool { return f.Name == name })
		field := webDOM(t, document.View{Body: []g.Node{core.Value(r.Resolved.Fields[i], want.Probe, o)}})
		body := webFind(field, func(n *html.Node) bool { return n.Data == "body" })[0]
		if webText(body) != text {
			t.Errorf("probe %s=%q, want %q", name, webText(body), text)
		}
		if tone := want.Tones[name]; tone != "" {
			pills := webFind(field, func(n *html.Node) bool { return webAttr(n, "data-component") == "badge" })
			if len(pills) != 1 || webAttr(pills[0], "data-tone") != tone {
				t.Errorf("probe %s tone", name)
			}
		}
	}
	for _, path := range want.Images {
		if len(webFind(record, func(n *html.Node) bool { return n.Data == "img" && webAttr(n, "src") == path })) != 1 {
			t.Errorf("missing picture %s", path)
		}
	}
	if want.Probe["id"] != nil && !r.Singleton && !r.CommandsOnly {
		list := webDOM(t, core.List(r, o, []map[string]any{want.Probe}, 1, 1, "", false))
		for _, word := range want.Absent {
			if strings.Contains(webText(list), word) || strings.Contains(webText(record), word) {
				t.Errorf("hidden probe value %q rendered", word)
			}
		}
		for _, path := range want.Images {
			if len(webFind(list, func(n *html.Node) bool { return n.Data == "img" && webAttr(n, "src") == path })) != 1 {
				t.Errorf("missing list picture %s", path)
			}
		}
	}
	for i, text := range want.Counts {
		list := webDOM(t, core.List(r, o, nil, int64(i+1), 1, "", false))
		if !strings.Contains(webText(list), text) {
			t.Errorf("count missing %q", text)
		}
	}
}
