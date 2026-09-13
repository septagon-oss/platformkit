package huma_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/problem"
	problemhuma "github.com/septagon-oss/platformkit/kit/problem/providers/huma"
)

// The shared type satisfies both framework response interfaces without the
// core package importing that framework.
var (
	_ huma.StatusError       = (*problem.Problem)(nil)
	_ huma.ContentTypeFilter = (*problem.Problem)(nil)
)

func TestHumaErrorKeepsClientDetailAndHidesServerCause(t *testing.T) {
	client, ok := problemhuma.NewError(http.StatusBadRequest, "invalid body", errors.New("name is required")).(*problem.Problem)
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
	server := problemhuma.NewError(http.StatusInternalServerError, "could not reach postgres", cause)
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
	outage := problemhuma.NewError(http.StatusServiceUnavailable, "policy store unreachable")
	if outage.GetStatus() != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", outage.GetStatus())
	}
	if strings.Contains(outage.Error(), "policy store") {
		t.Errorf("a 503 leaked its message: %q", outage.Error())
	}
}

func TestValidationErrorsKeepLocationsWithoutEchoingRequestValues(t *testing.T) {
	const password = "private registration passphrase"
	for _, value := range []any{password, map[string]any{"password": password}} {
		problem := problemhuma.NewError(422, "validation failed", &huma.ErrorDetail{
			Message: "expected a string", Location: "body.password", Value: value,
		})
		body, err := json.Marshal(problem)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), password) || !strings.Contains(string(body), "body.password") || !strings.Contains(string(body), "expected a string") {
			t.Fatal("validation must retain the field and explanation without repeating credentials")
		}
	}
}

// TestAServerErrorCarriesNoDetailAndAnUnknownStatusStillHasATitle. Repeating
// the title in the detail says nothing the status has not said, and a body with
// an empty title is not an RFC 9457 problem.
func TestAServerErrorCarriesNoDetailAndAnUnknownStatusStillHasATitle(t *testing.T) {
	// A framework-generated 5xx keeps its cause without copying it into the
	// response. The provider delegates sanitization to the shared constructor.
	body, err := json.Marshal(problemhuma.NewError(http.StatusInternalServerError, "", errors.New("dial tcp: refused")))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"type":"about:blank","title":"Internal Server Error","status":500}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}

	odd := problem.New(599, "")
	if odd.Title != "HTTP 599" {
		t.Errorf("title = %q, want %q", odd.Title, "HTTP 599")
	}
}
