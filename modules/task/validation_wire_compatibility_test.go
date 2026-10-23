package task_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/problem"
)

func TestTaskValidationKeepsLegacyWireFields(t *testing.T) {
	_, router := mounted(t)
	for _, tc := range []struct {
		name       string
		body       string
		detail     string
		errors     []string
		violations []fault.Violation
	}{
		{
			name: "schema", body: `{"title":"","priority":"unknown"}`, detail: "validation failed",
			errors: []string{
				`body.priority: expected value to be one of "low, normal, high, critical"`,
				"body.title: expected length >= 1",
			},
			violations: []fault.Violation{
				{Location: "priority", Code: "schema", Message: `expected value to be one of "low, normal, high, critical"`},
				{Location: "title", Code: "schema", Message: "expected length >= 1"},
			},
		},
		{
			name: "entity", body: `{"title":" ","status":"resolved"}`,
			detail: "crud: invalid: a task needs a title; a resolved task needs a resolution time",
			violations: []fault.Violation{
				{Location: "title", Code: "required", Message: "a task needs a title"},
				{Location: "resolvedAt", Code: "required", Message: "a resolved task needs a resolution time"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := call(t, router, http.MethodPost, path, tc.body)
			if code != http.StatusUnprocessableEntity {
				t.Fatalf("refusal: %d %s", code, body)
			}
			var got problem.Problem
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatal(err)
			}
			if got.Detail != tc.detail || !slices.Equal(got.Errors, tc.errors) {
				t.Errorf("legacy fields changed: %s; want detail=%q, errors=%q", body, tc.detail, tc.errors)
			}
			if !slices.Equal(got.Violations, tc.violations) {
				t.Errorf("violations = %+v, want %+v", got.Violations, tc.violations)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(body), &fields); err != nil {
				t.Fatal(err)
			}
			if _, present := fields["errors"]; present != (len(tc.errors) != 0) {
				t.Errorf("legacy errors presence changed: %s", body)
			}
		})
	}
}
