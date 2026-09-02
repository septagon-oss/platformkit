package problem_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/problem"
)

// A Problem is the error type huma writes, so it has to satisfy both of huma's
// response interfaces.
var (
	_ huma.StatusError       = (*problem.Problem)(nil)
	_ huma.ContentTypeFilter = (*problem.Problem)(nil)
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

func TestHumaErrorKeepsClientDetailAndHidesServerCause(t *testing.T) {
	client, ok := problem.HumaError(http.StatusBadRequest, "invalid body", errors.New("name is required")).(*problem.Problem)
	if !ok {
		t.Fatal("HumaError did not return a *Problem")
	}
	if client.Detail != "invalid body" {
		t.Errorf("detail = %q, want the caller's message", client.Detail)
	}
	if len(client.Errors) != 1 || client.Errors[0] != "name is required" {
		t.Errorf("errors = %v, want the validation detail", client.Errors)
	}

	cause := errors.New("dial tcp 10.0.0.1:5432: connection refused")
	server := problem.HumaError(http.StatusInternalServerError, "could not reach postgres", cause)
	if got := server.Error(); strings.Contains(got, "postgres") || strings.Contains(got, "10.0.0.1") {
		t.Errorf("a 5xx leaked its message to the client: %q", got)
	}
	if !errors.Is(server, cause) {
		t.Error("a 5xx dropped the cause the logger needs")
	}
	if server.GetStatus() != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", server.GetStatus())
	}

	// Hiding the message must not flatten the status: an outage that asks the
	// caller to retry is a different fact from one that does not.
	outage := problem.HumaError(http.StatusServiceUnavailable, "policy store unreachable")
	if outage.GetStatus() != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", outage.GetStatus())
	}
	if strings.Contains(outage.Error(), "policy store") {
		t.Errorf("a 503 leaked its message: %q", outage.Error())
	}
}
