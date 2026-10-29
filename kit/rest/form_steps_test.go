package rest_test

import (
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func stepsFixture() ([]entity.Field, present.ResolvedForm, rest.FormState) {
	fields := []entity.Field{
		{Name: "title", Type: entity.TypeString, Required: true, Default: "default"},
		{Name: "body", Type: entity.TypeText}, {Name: "rank", Type: entity.TypeInt},
		{Name: "pinned", Type: entity.TypeBool}, {Name: "tags", Type: entity.TypeList, Elem: entity.TypeString},
	}
	form := present.ResolvedForm{Layout: "steps", Steps: []present.ResolvedStep{
		{Key: "unsectioned", Fields: []string{"title"}}, {Key: "section:details", Fields: []string{"body", "rank"}}, {Key: "section:options", Fields: []string{"pinned", "tags"}},
	}}
	state := rest.FormState{Version: 1, Form: "/app/note/notes", Mode: "create", Actor: "actor", Tenant: "tenant", Schema: "digest", Answers: map[string]string{"title": "default"}}
	return fields, form, state
}

func stepBody(state rest.FormState, intent string, values url.Values) []byte {
	encoded, _ := json.Marshal(state)
	if values == nil {
		values = url.Values{}
	}
	values.Set("__pkit_form", string(encoded))
	values.Set("__pkit_intent", intent)
	return []byte(values.Encode())
}

func TestStepNavigationRetainsRawAnswersAndReplaysPosition(t *testing.T) {
	fields, form, initial := stepsFixture()
	state, values, err := rest.AdvanceForm(stepBody(initial, "next", url.Values{"title": {"Notebook"}}), initial, form, fields, false)
	if err != nil || state.Step != 1 || values != nil {
		t.Fatalf("first next: %+v %v %v", state, values, err)
	}
	body := stepBody(state, "next", url.Values{"body": {"draft"}, "rank": {"9007199254740993"}})
	state, _, err = rest.AdvanceForm(body, initial, form, fields, false)
	reload, _, again := rest.AdvanceForm(body, initial, form, fields, false)
	if err != nil || again != nil || state.Step != 2 || !reflect.DeepEqual(state, reload) {
		t.Fatalf("reload: %+v %v %v", state, err, again)
	}
	state, _, err = rest.AdvanceForm(stepBody(state, "back", url.Values{"tags": {"a, b"}}), initial, form, fields, false)
	if err != nil || state.Step != 1 || state.Answers["pinned"] != "false" || state.Answers["title"] != "Notebook" {
		t.Fatalf("back: %+v %v", state, err)
	}
	state, _, err = rest.AdvanceForm(stepBody(state, "next", url.Values{"body": {"edited"}, "rank": {"not-an-integer"}}), initial, form, fields, false)
	if err == nil || state.Step != 1 || state.Answers["rank"] != "not-an-integer" || state.Answers["tags"] != "a, b" {
		t.Fatalf("refusal lost raw input: %+v %v", state, err)
	}
	state, _, err = rest.AdvanceForm(stepBody(state, "back", url.Values{"body": {"edited"}, "rank": {"7"}}), initial, form, fields, false)
	if err != nil || state.Step != 0 || state.Answers["body"] != "edited" {
		t.Fatalf("back captures: %+v %v", state, err)
	}
}

func TestFinalSubmitRevalidatesCarriedFieldsBeforeReturningValues(t *testing.T) {
	fields, form, initial := stepsFixture()
	state := initial
	state.Step = 2
	state.Answers = map[string]string{"title": "", "body": "draft", "rank": "7"}
	refused, values, err := rest.AdvanceForm(stepBody(state, "submit", nil), initial, form, fields, true)
	if err == nil || refused.Step != 0 || values != nil || refused.Answers["title"] != "" {
		t.Fatalf("final required: %+v %v %v", refused, values, err)
	}
	state.Answers["title"] = "Notebook"
	state.Answers["rank"] = "9007199254740993"
	_, values, err = rest.AdvanceForm(stepBody(state, "submit", url.Values{"tags": {"a, b"}}), initial, form, fields, true)
	if err != nil || values["rank"] != int64(9007199254740993) || values["pinned"] != false || !reflect.DeepEqual(values["tags"], []string{"a", "b"}) {
		t.Fatalf("complete payload: %+v %v", values, err)
	}
	fields[4].Required = true
	_, _, err = rest.AdvanceForm(stepBody(initial, "next", url.Values{"title": {"Notebook"}}), initial, form, fields, false)
	if err != nil {
		t.Fatalf("next validated later required field: %v", err)
	}
	refused, _, err = rest.AdvanceForm(stepBody(state, "submit", nil), initial, form, fields, true)
	if err == nil || refused.Step != 2 {
		t.Fatalf("final skipped required tags: %+v %v", refused, err)
	}
}

func TestFormContinuationRefusesScopePolicyAndUnexpectedControls(t *testing.T) {
	fields, form, initial := stepsFixture()
	for _, tc := range []struct {
		name   string
		alter  func(*rest.FormState)
		values url.Values
		status int
	}{
		{"actor", func(s *rest.FormState) { s.Actor = "other" }, nil, 404},
		{"tenant", func(s *rest.FormState) { s.Tenant = "other" }, nil, 404},
		{"form", func(s *rest.FormState) { s.Form = "/other" }, nil, 404},
		{"schema", func(s *rest.FormState) { s.Schema = "other" }, nil, 409},
		{"version", func(s *rest.FormState) { s.Version = 2 }, nil, 409},
		{"step", func(s *rest.FormState) { s.Step = -1 }, nil, 422},
		{"unknown answer", func(s *rest.FormState) { s.Answers = map[string]string{"status": "done"} }, nil, 422},
		{"other step", func(*rest.FormState) {}, url.Values{"rank": {"7"}}, 422},
		{"duplicate", func(*rest.FormState) {}, url.Values{"title": {"one", "two"}}, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := initial
			tc.alter(&state)
			out, values, err := rest.AdvanceForm(stepBody(state, "next", tc.values), initial, form, fields, false)
			p, ok := errors.AsType[*problem.Problem](err)
			if !ok || p.Status != tc.status || out.Version != 0 || values != nil {
				t.Fatalf("unsafe continuation echoed: %+v %v %v", out, values, err)
			}
		})
	}
	for _, raw := range []string{`{"version":1,"version":1}`, `{"answers":{"title":"a","title":"b"}}`, `{}`, `null`, `{"Version":1}`, `{"version":1}}`} {
		out, _, err := rest.AdvanceForm([]byte(url.Values{"__pkit_form": {raw}, "__pkit_intent": {"next"}}.Encode()), initial, form, fields, false)
		if err == nil || out.Version != 0 {
			t.Fatalf("accepted malformed envelope %s", raw)
		}
	}
	_, _, err := rest.AdvanceForm([]byte(strings.Repeat("x", 1024*1024+1)), initial, form, fields, false)
	p, ok := errors.AsType[*problem.Problem](err)
	if !ok || p.Status != 413 {
		t.Fatalf("oversize: %v", err)
	}
}

func TestFormAnswerLabelsUseTheSameFieldChecksAsNext(t *testing.T) {
	fields := []entity.Field{{Name: "rank", Type: entity.TypeInt}, {Name: "pinned", Type: entity.TypeBool}, {Name: "choice", Type: entity.TypeString, Enum: []string{"one", "two"}}}
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"rank", "0", true}, {"rank", "9007199254740993", true}, {"rank", "bad", false}, {"rank", "9223372036854775808", false},
		{"pinned", "false", true}, {"pinned", "unknown", false}, {"choice", "one", true}, {"choice", "three", false}, {"choice", "", false},
	} {
		if got := rest.FormAnswerValid(map[string]string{tc.name: tc.value}, fields, tc.name); got != tc.valid {
			t.Fatalf("%s=%q valid=%v", tc.name, tc.value, got)
		}
	}
}

func TestInputStepsKeepExplicitBlankDistinctFromAnAbsentDefault(t *testing.T) {
	fields, form, expected := stepsFixture()
	expected.Mode = "input"
	state := expected
	state.Step = 2
	state.Answers = map[string]string{"title": "Notebook", "rank": ""}
	_, values, err := rest.AdvanceForm(stepBody(state, "submit", nil), expected, form, fields, true)
	value, present := values["rank"]
	if err != nil || !present || value != nil {
		t.Fatalf("blank lost its presence: %v %v", values, err)
	}
}
