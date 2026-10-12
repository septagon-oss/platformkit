package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/septagon-oss/platformkit/ui/screens"
)

// The browser journeys consume the generated form's accessible labels. A copy
// change must carry those consumers with it, including shared journey steps.
func TestBrowserJourneysAddressTheGeneratedFormLabels(t *testing.T) {
	for _, tc := range []struct{ module, entity, file, start, end string }{
		{"task", "task", "../../e2e/admin-tasks.spec.ts", "// Create.", "// Delete,"},
		{"content", "content", "../../e2e/steps/content.ts", "export async function fillContentForm", "export async function publishContent"},
	} {
		t.Run(tc.module, func(t *testing.T) {
			source, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			_, journey, found := strings.Cut(string(source), tc.start)
			if !found {
				t.Fatalf("cannot locate the form journey in %s", tc.file)
			}
			journey, _, found = strings.Cut(journey, tc.end)
			if !found {
				t.Fatalf("cannot locate the end of the form journey in %s", tc.file)
			}
			selectors := regexp.MustCompile(`getByLabel\('([^']+)'(?:,\s*\{\s*exact:\s*(true|false)\s*\})?\)`).FindAllStringSubmatch(journey, -1)
			if len(selectors) == 0 {
				t.Fatal("the journey supplies no label selectors to check")
			}
			form := screens.FormExample("journey-form", resourceAt(t, tc.module, tc.entity), screens.Options{}, "/save", "Create", nil, nil, "", true)
			var body strings.Builder
			if err := form.Node.Render(&body); err != nil {
				t.Fatal(err)
			}
			doc, err := html.Parse(strings.NewReader(body.String()))
			if err != nil {
				t.Fatal(err)
			}
			var labels []string
			for node := range doc.Descendants() {
				if node.Type != html.ElementNode || node.Data != "label" {
					continue
				}
				var text strings.Builder
				for child := range node.Descendants() {
					if child.Type == html.TextNode {
						text.WriteString(child.Data)
					}
				}
				labels = append(labels, strings.ToLower(strings.Join(strings.Fields(text.String()), " ")))
			}
			for _, selector := range selectors {
				matches := 0
				for _, label := range labels {
					want := strings.ToLower(selector[1])
					if (selector[2] == "true" && label == want) || (selector[2] != "true" && strings.Contains(label, want)) {
						matches++
					}
				}
				if matches != 1 {
					t.Errorf("%s asks for getByLabel(%q, exact=%s), which matches %d controls; want one of %v", tc.file, selector[1], selector[2], matches, labels)
				}
			}
		})
	}
}
