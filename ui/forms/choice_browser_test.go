package forms_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/forms"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

func browserChoice(t *testing.T, appearance string) forms.Field {
	f := choiceField(t, appearance, "many")
	c := f.Definition.Choices
	if appearance != "figure" {
		for i := 2; i < 13; i++ {
			c.Options = append(c.Options, entity.ChoiceOption{ID: "shape" + strconv.Itoa(i), Label: "Shape " + strconv.Itoa(i), Picture: &entity.ChoicePicture{Kind: "colour", Value: "#446677"}})
		}
		c.Options[2].Label = "Straße"
	}
	c.Options = append(c.Options, entity.ChoiceOption{ID: "none", Label: "No preference"})
	c.NoPreference = "none"
	c.Maximum = &entity.ChoiceMaximum{Count: 2, Behaviour: "replace-oldest"}
	if appearance == "chips" {
		c.Maximum = &entity.ChoiceMaximum{Count: 1, Behaviour: "refuse", Message: "Choose at most one shape."}
		c.Empty = entity.ChoiceEmpty{Mode: "refuse", Message: "Keep a shape."}
	}
	resolved := present.Resolve(present.Input{Schema: entity.Schema{Fields: []entity.Field{f.Definition}}})
	f.Resolved = &resolved.Fields[0]
	return f
}

func TestChoiceBrowserFixture(t *testing.T) {
	dir := os.Getenv("PK_CHOICE_FIXTURES")
	assets := ui.Assets(ui.Compose(design.Default()))
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"app.css", "js/components.js", "js/htmx.min.js"} {
			data, err := fs.ReadFile(assets, name)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, appearance := range []string{"tiles", "chips", "swatch", "figure"} {
		f := browserChoice(t, appearance)
		ex, err := forms.Example(appearance, forms.Model{Fields: []forms.Field{f}, ChoiceValues: map[string][]string{"shapes": {"round"}}}, forms.Options{Namespace: "fixture", Action: "/save", Title: "Choose shapes"})
		if err != nil {
			t.Fatal(err)
		}
		node := h.HTML(h.Lang("en"), h.Head(h.Meta(h.Charset("utf-8")), h.Meta(h.Name("viewport"), h.Content("width=device-width, initial-scale=1")), g.El("title", g.Text("Choice fixture")), h.Link(h.Rel("stylesheet"), h.Href("/app.css")), h.Script(h.Src("/htmx.min.js"), h.Defer()), h.Script(h.Src("/components.js"), h.Defer())), h.Body(h.Main(h.Class("mx-auto max-w-2xl p-6"), ex.Node)))
		var out strings.Builder
		out.WriteString("<!doctype html>")
		if err := node.Render(&out); err != nil {
			t.Fatal(err)
		}
		if dir != "" {
			if err := os.WriteFile(filepath.Join(dir, appearance+".html"), []byte(out.String()), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
