package resource

import (
	"slices"

	"github.com/septagon-oss/platformkit/kit/entity"
)

func (w Words) forms(h *entity.FormsHints) *entity.FormsHints {
	if h == nil {
		return nil
	}
	return &entity.FormsHints{Create: w.form("create", h.Create), Update: w.form("update", h.Update), Input: w.form("input", h.Input)}
}

func (w Words) form(mode string, h *entity.FormHints) *entity.FormHints {
	if h == nil {
		return nil
	}
	out := *h
	prefix := "form." + mode + "."
	out.UnsectionedLabel = w.say(prefix+"unsectioned.label", h.UnsectionedLabel)
	out.Steps = slices.Clone(h.Steps)
	for i, step := range out.Steps {
		key := "section:" + step.Section
		if step.Section == "" {
			key = "unsectioned"
		}
		out.Steps[i].PrimaryLabel = step.WithLabel(w.say(prefix+"step."+key+".primaryLabel", step.PrimaryLabel))
		out.Steps[i].EmptyLabel = w.say(prefix+"step."+key+".emptyLabel", step.EmptyLabel)
	}
	return &out
}
