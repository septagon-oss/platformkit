package forms_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/forms"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

func TestSketchBrowserInteractions(t *testing.T) {
	browser := os.Getenv("PLATFORMKIT_SKETCH_BROWSER")
	if browser == "" {
		t.Skip("set PLATFORMKIT_SKETCH_BROWSER to the installed Playwright package")
	}
	mux := http.NewServeMux()
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(ui.Assets(ui.Compose(design.Default())))))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		initial := r.URL.Query().Get("value")
		if initial == "" {
			initial = "null"
		}
		field := forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch}, Label: "Drawing", Editor: forms.Editor{Submitted: true}}
		extra := g.Group{}
		if r.URL.Query().Has("second") {
			other := field
			other.Label = "Other drawing"
			other.Editor.Submitted = false
			extra = append(extra, forms.MustExample("second", forms.Model{Fields: []forms.Field{other}}, forms.Options{Namespace: "second", Action: "/save", Title: "Other drawing", CancelURL: "/"}).Node)
		}
		editor := forms.MustExample("sketch", forms.Model{Fields: []forms.Field{field}, Values: map[string]string{"drawing": initial}, Errors: map[string]string{"drawing": r.URL.Query().Get("error")}}, forms.Options{Namespace: "sketch", Action: "/save", Title: "Drawing", CancelURL: "/"})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page := h.HTML(h.Lang("en"), h.Head(h.Meta(h.Charset("utf-8")), h.Meta(h.Name("viewport"), h.Content("width=device-width, initial-scale=1")), h.TitleEl(g.Text("Drawing")), h.Link(h.Rel("stylesheet"), h.Href("/assets/app.css")), h.Script(h.Src("/assets/js/sketch.js"), h.Type("module"))),
			h.Body(h.Main(components.Container(components.ContainerProps{MaxWidth: "sm"}, components.Heading(components.HeadingProps{Level: 1, Text: "Drawing"}), editor.Node, extra)), h.Script(h.Src("/assets/js/htmx.min.js")), h.Script(h.Src("/assets/js/htmx-config.js"))))
		if err := page.Render(w); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	cmd := exec.CommandContext(t.Context(), "node", "testdata/sketch-browser.mjs", browser, server.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
	if gate := os.Getenv("PLATFORMKIT_SKETCH_DESIGN_GATE"); gate != "" {
		cmd := exec.CommandContext(t.Context(), "python3", gate, server.URL, "--widths", "390,1440")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("design floor: %v\n%s", err, out)
		} else {
			t.Logf("python3 %s %s --widths 390,1440\n%s", gate, server.URL, out)
		}
	}
}
