package problem_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/problem"
)

func TestProblemIsAnRFC9457Body(t *testing.T) {
	p := problem.NotFound("no such task")
	if p.GetStatus() != http.StatusNotFound {
		t.Errorf("GetStatus = %d, want 404", p.GetStatus())
	}
	if got := p.Error(); got != "Not Found: no such task" {
		t.Errorf("Error = %q", got)
	}
	if got := p.ContentType("application/json"); got != problem.ContentType {
		t.Errorf("ContentType = %q, want %q", got, problem.ContentType)
	}
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"type":"about:blank","title":"Not Found","status":404,"detail":"no such task"}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestFromErrorKeepsCauseWithoutExposingIt(t *testing.T) {
	cause := errors.New("private connection details")
	for _, tc := range []struct {
		status int
		body   string
	}{
		{503, `{"type":"about:blank","title":"Service Unavailable","status":503}`},
		{409, `{"type":"about:blank","title":"Conflict","status":409}`},
		{200, `{"type":"about:blank","title":"OK","status":200}`},
		{599, `{"type":"about:blank","title":"HTTP 599","status":599}`},
		{-1, `{"type":"about:blank","title":"HTTP -1","status":-1}`},
	} {
		p := problem.FromError(tc.status, cause)
		body, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != tc.body || p.GetStatus() != tc.status || p.Detail != "" || p.Errors != nil || strings.Contains(p.Error(), cause.Error()) {
			t.Errorf("status %d exposed the wrong response: %s (%s)", tc.status, body, p.Error())
		}
		if !errors.Is(p, cause) || errors.Unwrap(p) != cause {
			t.Error("the sanitized response dropped its original cause")
		}
	}
	if p := problem.FromError(500, nil); p.Unwrap() != nil || p.Detail != "" || p.Errors != nil {
		t.Error("an absent cause introduced response details")
	}
}
