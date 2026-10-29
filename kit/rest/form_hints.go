package rest

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

func formHintsFault(r httpx.Resource) string {
	h := r.Present.Forms
	if h == nil {
		return ""
	}
	policy := present.Resolve(present.Input{Schema: r.Schema, Hints: r.Present, Immutable: r.Immutable,
		Operations: r.OperationWords(), Singleton: r.Singleton, InputOnly: r.InputRun != nil})
	inputNames := []string{}
	if r.InputRun != nil {
		for _, f := range r.Schema.Fields {
			if !entity.ServerOwned(f) && (!f.Presentation.Hidden() || (f.Required && f.Default == "")) {
				inputNames = append(inputNames, f.Name)
			}
		}
	}
	for _, form := range []struct {
		mode      string
		hint      *entity.FormHints
		fields    []string
		available bool
	}{
		{"create", h.Create, policy.Layout.Create, r.InputRun == nil && !r.Singleton && r.Offers(httpx.CRUDCreate)},
		{"update", h.Update, policy.Layout.Update, r.InputRun == nil && r.Offers(httpx.CRUDUpdate) && (!r.Singleton || r.Write != "")},
		{"input", h.Input, inputNames, r.InputRun != nil},
	} {
		if form.hint == nil {
			continue
		}
		if !form.available {
			return "form " + form.mode + " requires its operation"
		}
		if form.mode == "update" && form.hint.Layout == "steps" {
			if revisionColumn(r.Schema.Fields) == "" {
				return "update steps require an authoritative revision field"
			}
		}
		if _, err := present.ResolveForm(form.hint, r.Present.Sections, r.Schema.Fields, form.fields, "Details"); err != nil {
			return fmt.Sprintf("form %s: %v", form.mode, err)
		}
	}
	return ""
}
