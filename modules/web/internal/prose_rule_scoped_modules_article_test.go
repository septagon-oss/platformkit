package internal

// The module's own prose sheet moves from
// the end of the kernel's bytes into `@layer client`, and a later layer ranks
// above every earlier one whatever its selector says. Two things therefore hold
// this sheet in place, and nothing in the tree said either of them out loud:
//
// 1. Every prose rule is a descendant rule under this module's own [data-prose]
// hook. A bare `a { … }` in the client layer would beat the kernel utility
// on every link in the site bar (components.Link renders .text-… in the
// components layer) — the one direction the layer statement makes a client
// rule win, and the gate in ui.Compose refuses only names the kernel owns,
// so nothing but this module's own scoping keeps a prose rule inside the
// article.
// 2. Nothing inside [data-prose] carries a class. That is what makes the client
// layer's precedence harmless on this page: a Markdown render emits bare
// elements (goldmark escapes raw HTML, bluemonday.UGCPolicy sanitises the
// result, and parser.WithAttribute is not enabled, so an author cannot write
// a class), and the one component inside the article is the kernel's heading.
// The moment an authoring syntax for classes arrives, a prose rule and a
// utility can land on one element and the layer — not the selector — decides.
//
// Case 1 reads this module's composed sheet; case 2 reads what the content
// renderer can actually emit. Both are about this module, neither counts the
// tree.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	contracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// clientLayer returns the selectors inside the sheet's last @layer client block,
// and whether the client block is the last layer block in the sheet.
func clientLayer(t *testing.T, body string) (sels []string, last bool) {
	t.Helper()
	marker := "@layer client {"
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatal("the composed sheet has no @layer client block")
	}
	depth := 0
	end := -1
	for i := start + len(marker) - 1; i < len(body); i++ {
		switch body[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
				break
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		t.Fatal("the @layer client block never closes")
	}
	for _, line := range strings.Split(body[start+len(marker):end], "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, "{") && !strings.HasPrefix(line, "@") {
			for _, part := range strings.Split(strings.TrimSuffix(line, "{"), ",") {
				sels = append(sels, strings.TrimSpace(part))
			}
		}
	}
	for _, name := range []string{"tokens", "base", "components"} {
		if i := strings.LastIndex(body, "@layer "+name+" {"); i > start {
			last = false
			return
		}
	}
	last = true
	return
}

func TestEveryProseRuleIsScopedToTheModulesOwnArticle(t *testing.T) {
	sheet := ui.Compose(design.Default(), ui.Extra{Lists: lists(), Sheets: []*css.Sheet{prose()}})
	body := string(sheet.Body)

	var want []string
	if err := prose().WalkRules(func(selector string, _ []css.Declaration) error {
		for _, part := range strings.Split(selector, ",") {
			want = append(want, strings.TrimSpace(part))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	got, last := clientLayer(t, body)
	if strings.Index(body, "@layer tokens, base, components, client;") != 0 {
		t.Errorf("the sheet does not open with the layer order statement; it starts %q", body[:min(60, len(body))])
	}
	if !last {
		t.Error("@layer client is not the last layer block in the sheet, so the layer statement's order is not the order the sheet reads in")
	}
	if len(got) != len(want) || strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the client layer holds %d selectors %v, want exactly this module's prose rules %v: the client layer is this module's rules and nothing else",
			len(got), got, want)
	}
	for _, sel := range got {
		if sel != "[data-prose]" && !strings.HasPrefix(sel, "[data-prose] ") {
			t.Errorf("a prose rule (%s) is not the article's own hook or a descendant of it: it is in @layer client, which ranks above the components layer, so it would beat the kernel utility on any element of the site it names. This module owns the article, not the bar.", sel)
			continue
		}
		if sel == "[data-prose]" {
			continue
		}
		subject := sel[strings.LastIndex(sel, " ")+1:]
		if strings.ContainsAny(subject, ".[#") {
			t.Errorf("a prose rule (%s) names a class, an id or an attribute as its subject: it aims at markup this module does not render", sel)
		}
	}
}

func TestNothingInsideTheArticleCarriesAClass(t *testing.T) {
	html, err := contracts.Render(sampleArticleBody)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "<h1") || !strings.Contains(html, "<table") {
		t.Fatalf("the sample no longer reaches the elements the prose rules style: %s", html)
	}
	if i := strings.Index(html, "class="); i >= 0 {
		t.Errorf("the content renderer emitted class= at %d: an element inside [data-prose] can now carry a kernel utility class, and a prose rule and that class rank by layer, not by selector — re-read the prose sheet before shipping this. Fragment: %.120s", i, html[max(0, i-60):])
	}
}

const sampleArticleBody = "# Title\n\nA [link](https://example.com) with `code`.\n\n" +
	"# Heading with an attribute {.text-3xl}\n\n" +
	"> A quote.\n\n* one\n* two\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\n" +
	"<div class=\"text-3xl\">raw html</div>\n"

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
