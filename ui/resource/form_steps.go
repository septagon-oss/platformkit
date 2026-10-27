package resource

import (
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/document"
	"github.com/septagon-oss/platformkit/ui/forms"
	g "maragu.dev/gomponents"
)

// StepForm renders resolved membership supplied by the transport, with raw
// answers retained verbatim. The view is private and never uses field autosave.
func StepForm(r Resource, o Options, action, title string, values map[string]string, errs map[string]string, detail string, step forms.StepView) document.View {
	r = r.policy(o)
	if err := r.validatePolicy(); err != nil {
		return document.View{Body: []g.Node{invalid(err)}}
	}
	fields := make([]forms.Field, 0, len(r.Schema.Fields))
	words := o.Words(r.Schema)
	for _, f := range r.Schema.Fields {
		field := formField(words, f, o)
		if f.Type == entity.TypeBool {
			field.Options = []components.SelectOption{{Value: "true", Label: o.Text("screens.true", "Yes")}, {Value: "false", Label: o.Text("screens.false", "No")}}
		}
		fields = append(fields, field)
	}
	form := forms.StepExample(action, forms.Model{Fields: fields, Values: values, Errors: errs, Detail: detail}, forms.Options{
		Namespace: forms.Namespace(action), Action: action, Title: title, CancelURL: r.Screen,
		Text: func(key, fallback string) string {
			if o.Locale == nil {
				return fallback
			}
			return o.Locale.Text(key, fallback)
		},
	}, step)
	return document.View{Title: title, Language: r.Resolved.Language, Sensitive: true, Modules: append(richtextModules(r), "js/form-steps.js"), Body: []g.Node{
		breadcrumb(o, r.collectionName(), r.Screen, title),
		components.Toolbar(components.ToolbarProps{Title: title}), form.Node,
	}}
}
