package problem_test

import (
	"errors"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/problem"
)

func TestHumaErrorViolationsStripBody(t *testing.T) {
	t.Parallel()
	err := problem.HumaError(422, "validation failed",
		&huma.ErrorDetail{Message: "expected string", Location: "body.title"}, errors.New("plain"))
	p := err.(*problem.Problem)
	if len(p.Errors) != 2 || p.Errors[0] != "body.title: expected string" {
		t.Errorf("errors = %v", p.Errors)
	}
	want := []fault.Violation{{Location: "title", Code: "schema", Message: "expected string"}}
	if len(p.Violations) != 1 || p.Violations[0] != want[0] {
		t.Errorf("violations = %v", p.Violations)
	}
}
