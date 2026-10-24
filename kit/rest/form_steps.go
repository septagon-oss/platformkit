package rest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// FormState carries submitted raw answers, never authority. Compare its scope
// against the current request before rendering; revision remains the original.
type FormState struct {
	Version  int               `json:"version"`
	Form     string            `json:"form"`
	Mode     string            `json:"mode"`
	Record   string            `json:"record"`
	Tenant   string            `json:"tenant"`
	Actor    string            `json:"actor"`
	Schema   string            `json:"schema"`
	Step     int               `json:"step"`
	Revision string            `json:"revision,omitempty"`
	Answers  map[string]string `json:"answers"`
}

// FormDigest identifies editable structure, excluding localized copy and answers.
func FormDigest(mode string, fields []entity.Field, policy present.Resolved, form present.ResolvedForm) string {
	fs, steps := []any{}, []any{}
	for _, f := range fields {
		if !formHas(form, f.Name) {
			continue
		}
		keys := []any{}
		for _, k := range f.Keys {
			keys = append(keys, []any{k.Name, k.Widget, k.From})
		}
		control := ""
		for _, resolved := range policy.Fields {
			if resolved.Name == f.Name {
				control = resolved.Control
			}
		}
		enum := append([]string{}, f.Enum...)
		fs = append(fs, []any{f.Name, f.Type, f.Elem, f.Required, f.Default, f.MaxLength, control, enum, keys})
	}
	for _, step := range form.Steps {
		steps = append(steps, []any{step.Key, step.Fields})
	}
	encoded, _ := json.Marshal([]any{1, mode, fs, steps})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// AdvanceForm parses one bounded POST and returns its deterministic target.
// A zero state means unsafe input and must never be echoed. Nonzero state with
// an error retains raw answers for repair. Values are nonnil only on Submit.
func AdvanceForm(body []byte, expected FormState, form present.ResolvedForm, fields []entity.Field, final bool) (FormState, map[string]any, error) {
	state, post, err := decodeFormState(body, expected)
	if err != nil {
		return FormState{}, nil, err
	}
	if state.Step < 0 || state.Step >= len(form.Steps) || state.Answers == nil {
		return FormState{}, nil, problem.New(422, "Invalid form position")
	}
	intent := post.Get("__pkit_intent")
	if (final && (intent != "submit" || state.Step != len(form.Steps)-1)) ||
		(!final && intent != "next" && intent != "back") ||
		(intent == "next" && state.Step == len(form.Steps)-1) || (intent == "back" && state.Step == 0) {
		return FormState{}, nil, problem.New(422, "Invalid form action")
	}
	if err := captureStep(&state, post, form, fields); err != nil {
		return FormState{}, nil, err
	}
	if intent == "back" {
		state.Step--
		return state, nil, nil
	}
	values := map[string]any{}
	for _, f := range stepValidationFields(form, fields, state.Step, final) {
		if err := validateFormField(state.Answers, f, values); err != nil {
			errs, _ := FieldErrors(err, fields)
			state.Step = FormErrorStep(form, errs, state.Step)
			return state, nil, err
		}
	}
	if !final {
		state.Step++
		return state, nil, nil
	}
	if state.Mode == "update" {
		values[entity.RevisionCondition] = state.Revision
	}
	if state.Mode == "input" {
		for _, f := range fields {
			if raw, sent := state.Answers[f.Name]; sent && strings.TrimSpace(raw) == "" {
				if _, parsed := values[f.Name]; !parsed {
					values[f.Name] = nil
				}
			}
		}
	}
	return state, values, nil
}

// FormErrorStep chooses step order, then field order, regardless of error order.
// Unlocated errors leave the submitted position unchanged.
func FormErrorStep(form present.ResolvedForm, errs map[string]string, current int) int {
	for i, step := range form.Steps {
		for _, name := range step.Fields {
			if errs[name] != "" {
				return i
			}
		}
	}
	return current
}

// FormAnswerValid reports whether a label may interpolate this raw answer.
// It uses the same field checks as Next, without changing navigation or values.
func FormAnswerValid(raw map[string]string, fields []entity.Field, name string) bool {
	f, ok := entity.FieldNamed(fields, name)
	return ok && strings.TrimSpace(raw[name]) != "" && validateFormField(raw, f, map[string]any{}) == nil
}

func formHas(form present.ResolvedForm, name string) bool {
	return slices.ContainsFunc(form.Steps, func(s present.ResolvedStep) bool { return slices.Contains(s.Fields, name) })
}

func captureStep(state *FormState, post url.Values, form present.ResolvedForm, fields []entity.Field) error {
	allowed, current := map[string]bool{}, map[string]bool{}
	for _, f := range fields {
		if !formHas(form, f.Name) {
			continue
		}
		for _, name := range formControls(f) {
			allowed[name] = true
			if slices.Contains(form.Steps[state.Step].Fields, f.Name) {
				current[name] = true
			}
		}
	}
	for key := range state.Answers {
		if !allowed[key] {
			return problem.New(422, "Unknown retained form field")
		}
	}
	for key, values := range post {
		if len(values) != 1 || (!current[key] && key != "__pkit_form" && key != "__pkit_intent") {
			return problem.New(422, "Unexpected or repeated form field")
		}
	}
	state.Answers = maps.Clone(state.Answers)
	for name := range current {
		delete(state.Answers, name)
		if value, ok := post[name]; ok {
			state.Answers[name] = value[0]
		}
	}
	for _, f := range fields {
		if f.Type == entity.TypeBool && current[f.Name] {
			state.Answers[f.Name] = "false"
			if value := post.Get(f.Name); value != "" && value != "false" {
				state.Answers[f.Name] = value
			}
		}
	}
	return nil
}

func formControls(f entity.Field) []string {
	if f.Type != entity.TypeMap {
		return []string{f.Name}
	}
	names := make([]string, 0, len(f.Keys))
	for _, k := range f.Keys {
		names = append(names, f.Name+"["+k.Name+"]")
	}
	return names
}

func validateFormField(raw map[string]string, f entity.Field, out map[string]any) error {
	if f.Type == entity.TypeBool && raw[f.Name] != "" && raw[f.Name] != "true" && raw[f.Name] != "false" {
		return invalid(f.Name, "is not a boolean")
	}
	post := url.Values{}
	for _, name := range formControls(f) {
		if value, ok := raw[name]; ok {
			post.Set(name, value)
		}
	}
	if err := readFormField(post, f, formRead{update: true, arguments: true}, out); err != nil {
		return err
	}
	if number, ok := out[f.Name].(float64); ok && (math.IsNaN(number) || math.IsInf(number, 0)) {
		return invalid(f.Name, "is not a finite number")
	}
	text := strings.TrimSpace(raw[f.Name])
	if f.Required && text == "" && f.Type != entity.TypeBool && f.Type != entity.TypeMap {
		return invalid(f.Name, "is required")
	}
	if f.MaxLength > 0 && utf8.RuneCountInString(raw[f.Name]) > f.MaxLength {
		return invalid(f.Name, "is too long")
	}
	if text != "" && len(f.Enum) > 0 {
		values := []string{text}
		if f.Type == entity.TypeList {
			values = strings.Split(text, ",")
		}
		for _, value := range values {
			if !slices.Contains(f.Enum, strings.TrimSpace(value)) {
				return invalid(f.Name, "is not an offered choice")
			}
		}
	}
	return nil
}

func decodeFormState(body []byte, expected FormState) (FormState, url.Values, error) {
	var state FormState
	if len(body) > httpx.MaxBodyBytes {
		return state, nil, problem.New(http.StatusRequestEntityTooLarge, "Form input is too large")
	}
	post, err := url.ParseQuery(string(body))
	if err != nil || len(post["__pkit_form"]) != 1 || len(post["__pkit_intent"]) != 1 {
		return state, nil, problem.New(422, "Invalid form continuation")
	}
	raw := post.Get("__pkit_form")
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := uniqueJSON(json.NewDecoder(strings.NewReader(raw)), false); err != nil {
		return state, nil, problem.New(422, "Invalid form continuation")
	}
	if err := decoder.Decode(&state); err != nil {
		return FormState{}, nil, problem.New(422, "Invalid form continuation")
	}
	if state.Form != expected.Form || state.Mode != expected.Mode || state.Record != expected.Record || state.Tenant != expected.Tenant || state.Actor != expected.Actor {
		return FormState{}, nil, problem.New(404, "This form is not available")
	}
	if state.Version != 1 || state.Schema != expected.Schema {
		return FormState{}, nil, problem.New(409, "This form changed; start again")
	}
	if state.Mode == "update" {
		if _, err := parseTag(`"` + state.Revision + `"`); err != nil {
			return FormState{}, nil, invalid(entity.RevisionCondition, "is not a revision; reopen the form")
		}
	} else if state.Revision != "" {
		return FormState{}, nil, problem.New(422, "Unexpected revision")
	}
	return state, post, nil
}

func uniqueJSON(d *json.Decoder, answers bool) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return errors.New("expected object")
	}
	seen := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return fmt.Errorf("duplicate member")
		}
		if !answers && !slices.Contains([]string{"version", "form", "mode", "record", "tenant", "actor", "schema", "step", "revision", "answers"}, name) {
			return errors.New("unknown continuation member")
		}
		seen[name] = true
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return err
		}
		if !answers && name == "answers" {
			if err := uniqueJSON(json.NewDecoder(strings.NewReader(string(value))), true); err != nil {
				return err
			}
		}
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}

func stepValidationFields(form present.ResolvedForm, fields []entity.Field, current int, final bool) []entity.Field {
	out := []entity.Field{}
	for i, step := range form.Steps {
		if !final && i != current {
			continue
		}
		for _, name := range step.Fields {
			if field, ok := entity.FieldNamed(fields, name); ok {
				out = append(out, field)
			}
		}
	}
	return out
}
