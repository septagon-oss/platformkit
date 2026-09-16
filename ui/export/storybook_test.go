package export_test

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/export"
)

func TestStorybookSelectionAndPropertyMetadata(t *testing.T) {
	example := examples.ExampleOf(examples.ExampleInfo{ID: "product/heading", ComponentID: "heading"}, c.HeadingProps{Text: "A section", Level: 2, Size: 1}, c.Heading)
	book := export.Storybook{Examples: []examples.Example{example}}
	if err := book.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, found := book.Find("pk-ui.component.heading/1"); found {
		t.Fatal("unpublished example was discovered")
	}
	book.Examples = append(book.Examples, example)
	if book.Validate() == nil {
		t.Fatal("ambiguous identity accepted")
	}
	d, err := example.Describe()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.HTML, "<h2") || !strings.Contains(d.HTML, "text-3xl") {
		t.Fatalf("semantic and visual levels are coupled: %s", d.HTML)
	}
	var schema struct {
		Properties map[string]struct {
			Enum        []int  `json:"enum"`
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(d.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Properties["size"].Enum) != 7 || schema.Properties["size"].Description == "" {
		t.Fatal("size documentation is not source-derived")
	}
	if _, err := example.WithProps(json.RawMessage(`{"size":9}`)); err == nil {
		t.Fatal("an undocumented visual size was accepted")
	}
	edited, err := example.WithProps(json.RawMessage(`{"text":"Updated","size":3}`))
	if err != nil {
		t.Fatal(err)
	}
	code, err := edited.GoProps()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "usage.go", "package example\nfunc sample(){\n"+code+"\n}", 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(code, `"Updated"`) || !strings.Contains(code, "Size:  3") {
		t.Fatalf("copied code is stale: %s", code)
	}
}

func TestShapeOverridesUseTheSameTokensInCSSAndExport(t *testing.T) {
	pair := design.Default()
	pair.Light.Shape = design.Shape{ButtonRadius: "9999px", CardRadius: "0px"}
	pair.Dark.Shape = design.Shape{ButtonRadius: "1rem", ModalRadius: "0px"}
	book, err := export.Export(pair, []examples.Example{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		mode        int
		name, value string
	}{
		{0, "--pk-radius-button", "9999px"}, {0, "--pk-radius-card", "0px"},
		{0, "--pk-radius-modal", "1rem"}, {1, "--pk-radius-button", "1rem"}, {1, "--pk-radius-modal", "0px"},
	} {
		found := false
		for _, token := range book.Themes[test.mode].Tokens {
			if token.Name == test.name && token.Value == test.value && token.Type == "dimension" {
				found = true
			}
		}
		if !found || !strings.Contains(book.CSS, test.name+": "+test.value+";") {
			t.Errorf("shape missing from CSS or export: %+v", test)
		}
	}
	for _, selector := range []string{"[data-component=button]", "[data-component=card]", "[data-modal-panel]"} {
		if !strings.Contains(book.CSS, selector) {
			t.Errorf("no shape rule for %s", selector)
		}
	}
}
