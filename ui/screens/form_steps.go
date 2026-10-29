package screens

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/forms"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func stepped(r httpx.Resource, mode string) bool {
	if r.Present.Forms == nil {
		return false
	}
	h := r.Present.Forms.Create
	if mode == "input" {
		h = r.Present.Forms.Input
	}
	if mode == "update" {
		h = r.Present.Forms.Update
	}
	return h != nil && h.Layout == "steps"
}

func mountFormSteps(router *httpx.Router, shell page.Shell, o Options, r httpx.Resource) {
	for _, mode := range []string{"create", "update"} {
		if !stepped(r, mode) {
			continue
		}
		rel, initial := r.Path, r.Path+"/new"
		item := mode == "update" && !r.Singleton
		if mode == "update" {
			if item {
				rel += "/{id}"
			}
			initial = rel + "/edit"
		}
		prefix := "screen-" + r.Module + "-" + r.Entity + "-" + mode + "-steps-"
		route := func(name, method, path string) page.Route {
			return page.Route{ID: prefix + name, Method: method, Path: path, Summary: "The " + r.Entity + " form"}
		}
		if item {
			page.Serve(router, shell, route("start", "GET", initial), r.WriteAuth(), func(ctx context.Context, req page.Request, in *itemInput) (page.View, error) {
				return formStepRequest(ctx, req, o, r, mode, in.ID, nil, false)
			})
		} else {
			page.Serve(router, shell, route("start", "GET", initial), r.WriteAuth(), func(ctx context.Context, req page.Request, _ *page.Empty) (page.View, error) {
				return formStepRequest(ctx, req, o, r, mode, uuid.Nil, nil, false)
			})
		}
		for _, final := range []bool{false, true} {
			path, name := initial+"/steps", "navigate"
			if final {
				path, name = rel, "submit"
			}
			if item {
				page.Serve(router, shell, route(name, "POST", path), r.WriteAuth(), func(ctx context.Context, req page.Request, in *itemFormInput) (page.View, error) {
					return formStepRequest(ctx, req, o, r, mode, in.ID, in.RawBody, final)
				})
			} else {
				page.Serve(router, shell, route(name, "POST", path), r.WriteAuth(), func(ctx context.Context, req page.Request, in *formInput) (page.View, error) {
					return formStepRequest(ctx, req, o, r, mode, uuid.Nil, in.RawBody, final)
				})
			}
		}
		page.Serve(router, shell, route("restart", "GET", initial+"/steps"), r.WriteAuth(), func(_ context.Context, req page.Request, _ *page.Empty) (page.View, error) {
			return page.View{}, httpx.SeeOther(strings.TrimSuffix(req.Path, "/steps"))
		})
	}
}

func formStepRequest(ctx context.Context, req page.Request, o Options, r httpx.Resource, mode string, id uuid.UUID, body []byte, final bool) (page.View, error) {
	o = localized(o, req)
	r = Localise(r, words(o, req))
	policy := resolve(r, words(o, req), "", false)
	form := policy.Layout.Forms.Create
	action, record, navigation := r.Screen, "", r.Screen+"/new/steps"
	if mode == "input" {
		form = policy.Layout.Forms.Input
		navigation = action + "/steps"
	}
	var row map[string]any
	if mode == "update" {
		form, record = policy.Layout.Forms.Update, "singleton"
		if !r.Singleton {
			record, action = id.String(), r.Screen+"/"+id.String()
		}
		navigation = action + "/edit/steps"
		var err error
		row, err = r.Get(ctx, id)
		if err != nil {
			return page.View{}, err
		}
	}
	expected := rest.FormState{Version: 1, Form: action, Mode: mode, Record: record,
		Tenant: req.Tenant.ID.String(), Actor: req.Principal.UserID.String(), Schema: rest.FormDigest(mode, r.Schema.Fields, *policy, *form)}
	state := expected
	var err error
	if body == nil && !final && !strings.HasSuffix(req.Path, "/steps") {
		state.Answers = initialAnswers(r, *form, row, mode)
		if mode == "update" {
			state.Revision = rest.Text(row[entity.RevisionField])
		}
	} else {
		var values map[string]any
		state, values, err = rest.AdvanceForm(body, expected, *form, r.Schema.Fields, final)
		if state.Version == 0 {
			if p, ok := errors.AsType[*problem.Problem](err); ok && p.Status == http.StatusConflict {
				view := page.Fault(p.Status, p.Detail, strings.TrimSuffix(navigation, "/steps"), o.Text("forms.step.restart", "Start again"))
				view.Sensitive = true
				return view, nil
			}
			return page.View{}, err
		}
		if err == nil && final {
			if mode == "input" {
				var result any
				result, err = r.InputRun(ctx, values)
				if err == nil {
					return readInputResult(r, o, result)
				}
			} else if mode == "create" {
				row, err = r.Create(ctx, values)
				if err == nil {
					return page.View{}, httpx.SeeOther(action + "/" + rest.Text(row["id"]))
				}
			} else {
				_, err = r.Update(ctx, id, values)
				if err == nil {
					return page.View{}, httpx.SeeOther(action)
				}
			}
		}
	}
	if p, ok := errors.AsType[*problem.Problem](err); err != nil && (!ok || (p.Status != 422 && p.Status != 409 && p.Status != 412)) {
		return page.View{}, err
	}
	return stepResponse(r, o, req, state, *form, navigation, err), nil
}

func initialAnswers(r httpx.Resource, form present.ResolvedForm, row map[string]any, mode string) map[string]string {
	answers := map[string]string{}
	for _, step := range form.Steps {
		for _, name := range step.Fields {
			f, _ := entity.FieldNamed(r.Schema.Fields, name)
			if value, exists := row[name]; exists {
				answers[name] = rest.Text(value)
			} else if mode != "update" && f.Default != "" {
				answers[name] = f.Default
				if f.Type == entity.TypeList {
					var value any
					d := json.NewDecoder(strings.NewReader(f.Default))
					d.UseNumber()
					if d.Decode(&value) == nil {
						answers[name] = rest.Text(value)
					}
				}
			} else if f.Type == entity.TypeBool {
				answers[name] = "false"
			}
		}
	}
	return answers
}

func stepResponse(r httpx.Resource, o Options, req page.Request, state rest.FormState, form present.ResolvedForm, navigation string, err error) page.View {
	var errs map[string]string
	detail, status := "", 0
	if err != nil {
		errs, detail = rest.FieldErrorsIn(err, r.Schema.Fields, words(o, req))
		detail, o.Refusal = refusedWrite(req.Locale, err, detail)
		state.Step = rest.FormErrorStep(form, errs, state.Step)
		if p, ok := errors.AsType[*problem.Problem](err); ok {
			status = p.Status
		}
	}
	encoded, _ := json.Marshal(state)
	title := o.Text("screens.new", "New %s", entryName(r, o))
	if state.Mode == "update" {
		title = o.Text("screens.edit_item", "Edit %s", entryName(r, o))
	}
	if state.Mode == "input" {
		title = entryName(r, o)
	}
	steps := make([]forms.Step, len(form.Steps))
	for i, step := range form.Steps {
		steps[i] = forms.Step{Key: step.Key, Label: step.Label, Fields: step.Fields,
			PrimaryLabel: step.PrimaryLabel, AnswerField: step.AnswerField, EmptyLabel: step.EmptyLabel}
	}
	view := resource.StepForm(described(r, o), o, state.Form, title, state.Answers, errs, detail,
		forms.StepView{Steps: steps, Current: state.Step, Continuation: string(encoded), Navigation: navigation,
			AnswerInvalid: form.Steps[state.Step].AnswerField != "" && !rest.FormAnswerValid(state.Answers, r.Schema.Fields, form.Steps[state.Step].AnswerField)})
	view.Status = status
	return view
}
