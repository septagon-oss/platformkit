package main

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/task/domain"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/forms"
	"golang.org/x/text/language"
	"golang.org/x/text/message/catalog"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

type note struct {
	entity.Base
	Title    string    `json:"title" validate:"required" doc:"A useful name."`
	Body     string    `json:"body" ui:"widget:textarea" doc:"Describe the result."`
	Priority string    `json:"priority" enum:"normal,high" default:"normal" doc:"Choose urgency."`
	Accepted bool      `json:"accepted" validate:"required" doc:"Review this choice."`
	When     time.Time `json:"when" doc:"A caller-owned local date and time."`
}

func (*note) TableName() string { return "external_notes" }

type view struct {
	values map[string]string
	errors map[string]string
	detail string
}

type receipt struct {
	Instance string            `json:"instance"`
	Values   map[string]string `json:"values"`
	Status   int               `json:"status"`
	Decision domain.Resolution `json:"decision"`
}

type application struct {
	messages locale.Messages
	mu       sync.Mutex
	views    map[string]view
	receipts []receipt
}

func newApplication() *application {
	builder := catalog.NewBuilder(catalog.Fallback(language.English))
	for _, entry := range []struct{ key, en, pt string }{
		{"title", "Title", "Título"}, {"body", "Description", "Descrição"},
		{"priority", "Priority", "Prioridade"}, {"accepted", "I confirm", "Confirmo"},
		{"when", "When", "Quando"}, {"save", "Save", "Guardar"},
		{"cancel", "Cancel", "Cancelar"}, {"normal", "Normal", "Normal"}, {"high", "High", "Alta"},
	} {
		if err := builder.SetString(language.English, entry.key, entry.en); err != nil {
			panic(err)
		}
		if err := builder.SetString(language.EuropeanPortuguese, entry.key, entry.pt); err != nil {
			panic(err)
		}
	}
	return &application{messages: xtext.FromCatalog(builder), views: map[string]view{
		"first-editor":  {values: map[string]string{"title": "Alpha draft", "body": "First details", "priority": "normal", "accepted": "false", "when": "2026-09-13T12:30"}, errors: map[string]string{"accepted": "Confirm this choice"}},
		"second-editor": {values: map[string]string{"title": "Beta draft", "body": "Second details", "priority": "normal", "accepted": "false", "when": "2026-09-14T13:45"}, errors: map[string]string{"body": "Add more details"}},
	}}
}

func (a *application) form(namespace string) components.Example {
	preference := "en"
	if namespace == "second-editor" {
		preference = "pt-PT"
	}
	selected := locale.SelectLocale(a.messages, preference)
	fields := []forms.Field{}
	for _, definition := range entity.Fields[*note]() {
		field := forms.Field{Definition: definition, Label: selected.Text(definition.Name, definition.Name)}
		for _, value := range definition.Enum {
			field.Options = append(field.Options, components.SelectOption{Value: value, Label: selected.Text(value, value)})
		}
		fields = append(fields, field)
	}
	v := a.views[namespace]
	form, err := forms.Example("external/"+namespace, forms.Model{Fields: fields,
		Values: v.values, Errors: v.errors, Detail: v.detail}, forms.Options{
		Namespace: namespace, Action: "/save/" + namespace, CancelURL: "/", Title: "Editor " + namespace,
		SubmitLabel: selected.Text("save", "Save"), CancelLabel: selected.Text("cancel", "Cancel"),
	})
	if err != nil {
		panic(err)
	}
	return form
}

func (a *application) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(ui.Assets(ui.Compose(design.Default())))))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page := h.HTML(h.Lang("en"), h.Head(h.Meta(h.Charset("utf-8")), h.Meta(h.Name("viewport"), h.Content("width=device-width, initial-scale=1")), h.TitleEl(g.Text("Independent package assembly")),
			h.Link(h.Rel("stylesheet"), h.Href("/assets/app.css")),
			h.Script(h.Src("/assets/js/htmx.min.js"), h.Defer()), h.Script(h.Src("/assets/js/htmx-config.js"), h.Defer()), h.Script(h.Src("/assets/js/components.js"), h.Defer())),
			h.Body(components.SkipLink("content", "Skip to editors"), h.Main(h.ID("content"),
				components.Heading(components.HeadingProps{Level: 1, Text: "Independent editors"}),
				components.Grid(components.GridProps{Columns: "1", MD: "2", Gap: "6"},
					h.Section(g.Attr("aria-label", "First editor"), a.form("first-editor").Node),
					h.Section(h.Lang("pt-PT"), g.Attr("aria-label", "Second editor"), a.form("second-editor").Node)))))
		if _, err := fmt.Fprint(w, "<!doctype html>"); err != nil {
			return
		}
		if err := page.Render(w); err != nil {
			log.Print(err)
		}
	})
	mux.HandleFunc("POST /save/{instance}", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		namespace := r.PathValue("instance")
		if _, exists := a.views[namespace]; !exists {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		values := map[string]string{}
		for _, name := range []string{"title", "body", "priority", "accepted", "when"} {
			values[name] = r.FormValue(name)
		}
		status := http.StatusOK
		next := view{values: values}
		decision := domain.Resolution{}
		if values["title"] == "reject" {
			status = http.StatusUnprocessableEntity
			next.errors, next.detail = map[string]string{"body": "Explain the result before saving"}, "The external controller refused this draft."
		} else {
			var err error
			decision, err = domain.Resolve(domain.StatusOpen, "", values["title"])
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		a.views[namespace] = next
		a.receipts = append(a.receipts, receipt{Instance: namespace, Values: maps.Clone(values), Status: status, Decision: decision})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		if err := a.form(namespace).Node.Render(w); err != nil {
			log.Print(err)
		}
	})
	mux.HandleFunc("GET /snapshot", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		snapshot, err := ui.Export(design.Default(), []components.Example{a.form("first-editor"), a.form("second-editor")})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(snapshot); err != nil {
			log.Print(err)
		}
	})
	mux.HandleFunc("GET /receipts", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(a.receipts); err != nil {
			log.Print(err)
		}
	})
	return mux
}

func main() {
	server := &http.Server{Addr: "127.0.0.1:18141", Handler: newApplication().handler(), ReadHeaderTimeout: 5 * time.Second}
	log.SetOutput(os.Stderr)
	log.Fatal(server.ListenAndServe())
}
