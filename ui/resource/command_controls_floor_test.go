package resource_test

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestQuietCommandControlsKeepTheirNativeSubmitAndAccessibleName(t *testing.T) {
	for name, page := range map[string]string{
		"record": rowPage(t, withCommands(assignCommand, sweepCommand)),
		"list":   listPage(t, withCommands(assignCommand, sweepCommand)),
	} {
		t.Run(name, func(t *testing.T) {
			doc, err := html.Parse(strings.NewReader(page))
			if err != nil {
				t.Fatal(err)
			}
			action, label := assignCommand.Action("/app/note/notes/11111111-1111-1111-1111-111111111111"), assignCommand.Label
			if name == "list" {
				action, label = sweepCommand.Action(""), sweepCommand.Label
			}
			attrs := func(n *html.Node) map[string]string {
				out := make(map[string]string)
				for _, a := range n.Attr {
					out[a.Key] = a.Val
				}
				return out
			}
			forms, submits := 0, 0
			for n := range doc.Descendants() {
				if n.Type != html.ElementNode || n.Data != "form" || attrs(n)["action"] != action {
					continue
				}
				forms++
				if attrs(n)["method"] != "post" || attrs(n)["aria-label"] != label {
					t.Errorf("command form lost its POST method or accessible name: %v", attrs(n))
				}
				for control := range n.Descendants() {
					if control.Type != html.ElementNode || control.Data != "button" || attrs(control)["type"] != "submit" {
						continue
					}
					submits++
					if attrs(control)["data-variant"] != "outline" {
						t.Errorf("command submit must keep the quiet outline variant: %v", attrs(control))
					}
				}
			}
			if forms != 1 || submits != 1 {
				t.Errorf("command %s has %d forms and %d native submits; want one each", action, forms, submits)
			}
		})
	}
}
