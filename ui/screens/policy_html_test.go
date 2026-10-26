package screens_test

import (
	"encoding/json"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/ui/document"
	core "github.com/septagon-oss/platformkit/ui/resource"
	"golang.org/x/net/html"
	g "maragu.dev/gomponents"
)

type webExpectation struct {
	SourceInputHash, SourceExpectedHash        string
	Heading, Empty                             string
	Columns, SortLabels, Overview, Information []string
	Probe                                      map[string]any
	Readings, Tones                            map[string]string
	Absent, Images, Counts                     []string
	EmptyTitle                                 string
}

// webUnasserted names every fixture of the shared corpus whose rendered HTML this
// companion does not assert, with what does assert it today. A fixture absent here
// and absent from the companion refuses: the corpus grows one fixture at a time,
// and a screen nobody renders has to be a named decision rather than a skip line.
var webUnasserted = map[string]string{
	"choice-axes-command": "its resolved model and rows are asserted by the resolver's own fixture run",
}

func TestEveryParityFixtureRendersItsSemanticModel(t *testing.T) {
	var companions struct{ Cases map[string]webExpectation }
	raw, err := os.ReadFile("testdata/parity/web/expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &companions); err != nil {
		t.Fatal(err)
	}
	fixtures := readParity(t)
	asserted := make(map[string]bool, len(fixtures))
	corpus := make(map[string]bool, len(fixtures))
	for _, fixture := range fixtures {
		corpus[fixture.Name] = true
		want, ok := companions.Cases[fixture.Name]
		if !ok {
			why, named := webUnasserted[fixture.Name]
			if !named {
				t.Errorf("fixture %s has no web companion and no name in the list of unasserted screens", fixture.Name)
				continue
			}
			// A named screen is skipped rather than passing as asserted, and its
			// source keeps the hashes its own author wrote.
			t.Run(fixture.Name, func(t *testing.T) {
				t.Skipf("%s: no web companion, its HTML is unasserted: %s", fixture.Name, why)
			})
			continue
		}
		asserted[fixture.Name] = true
		t.Run(fixture.Name, func(t *testing.T) {
			if want.SourceInputHash != fixture.Provenance.InputHash || want.SourceExpectedHash != fixture.Provenance.ExpectedHash {
				t.Fatal("HTML companion is absent or its source changed")
			}
			var in parityInput
			if err := json.Unmarshal(fixture.Input, &in); err != nil {
				t.Fatal(err)
			}
			var expected struct {
				Resolved map[string]json.RawMessage
				Rows     map[string]parityRow
			}
			if err := json.Unmarshal(fixture.Expected, &expected); err != nil {
				t.Fatal(err)
			}
			policy := in.resolve()
			r := core.Resource{Schema: in.Schema, Present: in.Hints, Immutable: in.Immutable, Singleton: in.Singleton, CommandsOnly: in.CommandsOnly, Operations: in.Operations, Resolved: &policy, Screen: "/entries"}
			for _, c := range in.Commands {
				r.Commands = append(r.Commands, core.Command{Verb: c.Verb, Label: c.Summary, Present: c.Hints, Collection: true, Base: r.Screen})
			}
			view := document.View{Body: core.CommandForms(r, opts, "/entries", true, nil)}
			if !in.Singleton && !in.CommandsOnly {
				view = core.List(r, opts, in.Rows, int64(len(in.Rows)), 1, "", true)
			}
			list := webDOM(t, view)
			equalHTML(t, "columns", webTexts(list, "th"), want.Columns)
			if want.Heading != "" {
				equalHTML(t, "heading", webTexts(list, "h1"), []string{want.Heading})
			}
			if want.Empty != "" && !strings.Contains(webText(list), want.Empty) {
				t.Errorf("missing empty copy %q", want.Empty)
			}
			checkHTMLRows(t, r, in, expected.Rows, list)
			checkHTMLLayout(t, r, expected.Resolved["layout"], want)
			checkHTMLSorts(t, list, expected.Resolved["list"], want.SortLabels)
			checkHTMLCommands(t, list, expected.Resolved["commands"])
			checkHTMLOptions(t, r, expected.Resolved["fields"])
			checkHTMLProbe(t, r, want)
		})
	}
	for name := range companions.Cases {
		if !asserted[name] {
			t.Errorf("web companion %s names no fixture of the corpus", name)
		}
	}
	for name := range webUnasserted {
		if !corpus[name] {
			t.Errorf("unasserted screen %s names no fixture of the corpus", name)
		}
		if _, isCompanion := companions.Cases[name]; isCompanion {
			t.Errorf("unasserted screen %s names a fixture the companion asserts", name)
		}
	}
}

func checkHTMLRows(t *testing.T, r core.Resource, in parityInput, expected map[string]parityRow, list *html.Node) {
	t.Helper()
	for _, row := range in.Rows {
		id := row["id"].(string)
		want := expected[id]
		links := webFind(list, func(n *html.Node) bool { return n.Data == "a" && webAttr(n, "href") == "/entries/"+id })
		if len(links) != 1 || webText(links[0]) != want.ListTitle {
			t.Errorf("row %s link does not say %q", id, want.ListTitle)
		}
		record := webDOM(t, core.Detail(r, opts, row, false))
		equalHTML(t, "record title", webTexts(record, "title"), []string{want.RecordTitle + " · Probe"})
		cells := webFind(list, func(n *html.Node) bool { return n.Data == "tr" && webAttr(n, "data-pk-row") == id })
		if len(cells) != 1 {
			t.Fatalf("row %s absent", id)
		}
		cellText := webTexts(cells[0], "td")

		for name, text := range want.Cells {
			i := slices.IndexFunc(r.Resolved.Fields, func(f present.ResolvedField) bool { return f.Name == name })
			column := slices.Index(r.Resolved.Layout.Columns, name)
			if column > 0 && cellText[column] != text {
				t.Errorf("list %s.%s=%q, want %q", id, name, cellText[column], text)
			}
			field := webDOM(t, document.View{Body: []g.Node{core.Value(r.Resolved.Fields[i], row, opts)}})
			if got := webText(webFind(field, func(n *html.Node) bool { return n.Data == "body" })[0]); got != text {
				t.Errorf("%s.%s=%q, want %q", id, name, got, text)
			}
		}
	}
}

func checkHTMLLayout(t *testing.T, r core.Resource, raw json.RawMessage, want webExpectation) {
	t.Helper()
	var layout map[string][]string
	var full struct{ Create, Update []string }
	_ = json.Unmarshal(raw, &full)
	_ = json.Unmarshal(raw, &layout)
	for _, create := range []bool{true, false} {
		key := "update"
		if create {
			key = "create"
		}
		if _, ok := layout[key]; !ok {
			continue
		}
		doc := webDOM(t, core.Form(r, opts, "/entries", "Edit", nil, nil, "", create))
		names := []string{}
		for _, node := range webFind(doc, func(n *html.Node) bool {
			return (n.Data == "input" || n.Data == "select" || n.Data == "textarea") && webAttr(n, "name") != "" && !webHas(n, "disabled") && !webHas(n, "readonly")
		}) {
			names = append(names, webAttr(node, "name"))
		}
		equalHTML(t, key, names, layout[key])
	}
	record := webDOM(t, core.Detail(r, opts, map[string]any{"id": "probe"}, false))
	for role, terms := range map[string][]string{"overview": want.Overview, "record-information": want.Information} {
		if terms == nil {
			continue
		}
		blocks := webFind(record, func(n *html.Node) bool { return webAttr(n, "data-semantic-role") == role })
		if len(blocks) != 1 {
			t.Fatalf("%s blocks=%d", role, len(blocks))
		}
		equalHTML(t, role, webTexts(blocks[0], "dt"), terms)
	}
}

func checkHTMLSorts(t *testing.T, doc *html.Node, raw json.RawMessage, labels []string) {
	t.Helper()
	var expected struct {
		Sorts   []struct{ Query string }
		Filters []struct {
			Field  string
			Values []struct{ Value, Label string }
		}
	}
	if err := json.Unmarshal(raw, &expected); err != nil && len(raw) > 0 {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(raw, &keys)
	queries, words := []string{}, []string{}
	for _, node := range webFind(doc, func(n *html.Node) bool { return n.Data == "a" && webAttr(n, "data-component") == "button" }) {
		u, err := url.Parse(webAttr(node, "href"))
		if err != nil {
			t.Fatal(err)
		}
		if q := u.Query(); q.Get("sort") != "" && len(q["filter"]) == 0 {
			queries = append(queries, q.Get("sort"))
			words = append(words, webText(node))
		}
	}
	if _, ok := keys["sorts"]; ok {
		want := []string{}
		for _, choice := range expected.Sorts {
			want = append(want, choice.Query)
		}
		equalHTML(t, "sort queries", queries, want)
	}
	if labels != nil {
		equalHTML(t, "sort labels", words, labels)
	}
	for _, filter := range expected.Filters {
		for _, option := range filter.Values {
			if option.Label == "" {
				continue
			}
			found := false
			for _, node := range webFind(doc, func(n *html.Node) bool { return n.Data == "a" }) {
				u, _ := url.Parse(webAttr(node, "href"))
				if strings.HasSuffix(webText(node), ": "+option.Label) && (option.Value == "" || slices.Contains(u.Query()["filter"], filter.Field+":"+option.Value)) {
					found = true
				}
			}
			if !found {
				t.Errorf("filter %s lacks %s", filter.Field, option.Label)
			}
		}
	}
}

func checkHTMLCommands(t *testing.T, doc *html.Node, raw json.RawMessage) {
	t.Helper()
	var commands []struct {
		Verb, Label                  string
		Visible                      *bool
		System, Primary, Destructive bool
		Confirmation                 *struct{ Title, ConfirmLabel string }
	}
	_ = json.Unmarshal(raw, &commands)
	for _, c := range commands {
		forms := webFind(doc, func(n *html.Node) bool { return n.Data == "form" && webAttr(n, "action") == "/entries/"+c.Verb })
		if c.System || c.Visible != nil && !*c.Visible {
			if len(forms) > 0 {
				t.Errorf("hidden command %s rendered", c.Verb)
			}
			continue
		}
		if len(forms) != 1 {
			t.Fatalf("%s forms=%d", c.Verb, len(forms))
		}
		label, button := c.Label, c.Label
		if c.Confirmation != nil {
			if c.Confirmation.Title != "" {
				label = c.Confirmation.Title
			}
			if c.Confirmation.ConfirmLabel != "" {
				button = c.Confirmation.ConfirmLabel
			}
		}
		if webAttr(forms[0], "aria-label") != label {
			t.Errorf("command %s label", c.Verb)
		}
		buttons := webFind(forms[0], func(n *html.Node) bool { return n.Data == "button" && webAttr(n, "type") == "submit" })
		if len(buttons) != 1 || webText(buttons[0]) != button {
			t.Fatalf("command %s submit", c.Verb)
		}
		if c.Primary && webAttr(buttons[0], "data-variant") != "primary" {
			t.Error("primary command not emphasized")
		}
		if c.Destructive && webAttr(buttons[0], "data-tone") != "danger" {
			t.Error("destructive command not danger")
		}
	}
}

func checkHTMLOptions(t *testing.T, r core.Resource, raw json.RawMessage) {
	t.Helper()
	var fields []struct {
		Name    string
		Options []struct{ Value, Label, Tone string }
	}
	_ = json.Unmarshal(raw, &fields)
	form := webDOM(t, core.Form(r, opts, "/entries", "New", nil, nil, "", true))
	for _, f := range fields {
		for _, option := range f.Options {
			index := slices.IndexFunc(r.Resolved.Fields, func(p present.ResolvedField) bool { return p.Name == f.Name })
			read := webDOM(t, document.View{Body: []g.Node{core.Value(r.Resolved.Fields[index], map[string]any{f.Name: option.Value}, opts)}})
			pills := webFind(read, func(n *html.Node) bool { return webAttr(n, "data-component") == "badge" })
			if len(pills) != 1 {
				t.Fatalf("%s option has no pill", f.Name)
			}
			if option.Label != "" && webText(pills[0]) != option.Label {
				t.Errorf("%s option label", f.Name)
			}
			if option.Tone != "" && webAttr(pills[0], "data-tone") != option.Tone {
				t.Errorf("%s option tone", f.Name)
			}
			if option.Label != "" {
				nodes := webFind(form, func(n *html.Node) bool { return n.Data == "option" && webAttr(n, "value") == option.Value })
				if len(nodes) != 1 || webText(nodes[0]) != option.Label {
					t.Errorf("%s form option", f.Name)
				}
			}
		}
	}
}

func webDOM(t *testing.T, v document.View) *html.Node {
	t.Helper()
	var b strings.Builder
	if err := document.Document(document.Chrome{Brand: "Probe"}, document.Request{}, v, g.Group(v.Body)).Render(&b); err != nil {
		t.Fatal(err)
	}
	node, err := html.Parse(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	return node
}
func webAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func webHas(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}
func webText(n *html.Node) string {
	if webAttr(n, "aria-hidden") == "true" {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(webText(c))
	}
	return b.String()
}
func webFind(n *html.Node, keep func(*html.Node) bool) []*html.Node {
	out := []*html.Node{}
	for node := range n.Descendants() {
		if node.Type == html.ElementNode && keep(node) {
			out = append(out, node)
		}
	}
	return out
}
func webTexts(n *html.Node, tag string) []string {
	out := []string{}
	for _, node := range webFind(n, func(n *html.Node) bool { return n.Data == tag }) {
		out = append(out, webText(node))
	}
	return out
}
func equalHTML(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s=%q, want %q", what, got, want)
	}
}
