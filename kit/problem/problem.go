// Package problem is the one error shape the API returns: RFC 9457 problem
// details (the revision of RFC 7807), served as application/problem+json.
//
// A Problem is an error, so a handler returns one; the huma hook in huma.go
// turns every framework error into the same shape, which is why there is no
// second error type anywhere in the kernel.
//
// Derived from github.com/septagon-oss/pk-problem (Apache-2.0); see NOTICE.
package problem

import (
	"fmt"
	"net/http"
)

// ContentType is the media type of a problem response.
const ContentType = "application/problem+json"

// Problem is an RFC 9457 problem details object.
type Problem struct {
	// Type identifies the problem type. "about:blank" means the status code
	// says everything there is to say.
	Type string `json:"type"`
	// Title is the status text: the same for every occurrence of a type.
	Title string `json:"title"`
	// Status is the HTTP status code.
	Status int `json:"status"`
	// Detail explains this occurrence.
	Detail string `json:"detail,omitempty"`
	// Errors carries per-field validation messages, when there are any.
	Errors []string `json:"errors,omitempty"`
	// Instance identifies this occurrence. kit/httpx sets it to
	// "urn:request:<request id>", which is the same id the log line carries, so
	// a report of "I got a 500" is one grep away from the reason.
	Instance string `json:"instance,omitempty"`

	// cause is the server-side error. It is never serialized; Unwrap exposes
	// it to the logger, which is the only thing allowed to see it.
	cause error
}

// New returns a problem with the standard title for status. A status net/http
// has no text for still gets a title, because a body with an empty one is not
// an RFC 9457 problem.
func New(status int, detail string) *Problem {
	title := http.StatusText(status)
	if title == "" {
		title = fmt.Sprintf("HTTP %d", status)
	}
	return &Problem{Type: "about:blank", Title: title, Status: status, Detail: detail}
}

// New is the constructor for every status; the two below are the only ones with
// a name of their own, because they are the two kit/crud returns often enough
// that spelling the status at each site would be the thing that goes wrong.
//
// NotFound is 404: no such thing, or none this tenant may see.
func NotFound(detail string) *Problem { return New(http.StatusNotFound, detail) }

// Conflict is 409: the request contradicts the current state.
func Conflict(detail string) *Problem { return New(http.StatusConflict, detail) }

// Internal is 500. It carries no detail at all: a server error's real message
// belongs in the log, and repeating the title in the detail says nothing the
// status has not already said.
func Internal(cause error) *Problem { return serverError(http.StatusInternalServerError, cause) }

// serverError is any 5xx: the status is kept, because 503 asks a caller to come
// back and 500 asks them not to, and the message is not, for the same reason
// Internal withholds it.
func serverError(status int, cause error) *Problem {
	p := New(status, "")
	p.cause = cause
	return p
}

// Error makes a Problem usable as an error.
func (p *Problem) Error() string {
	if p.Detail == "" {
		return p.Title
	}
	return p.Title + ": " + p.Detail
}

// Unwrap exposes the server-side cause to logging and errors.Is.
func (p *Problem) Unwrap() error { return p.cause }

// GetStatus satisfies huma.StatusError, so returning a Problem from a handler
// sets the response status.
func (p *Problem) GetStatus() int { return p.Status }

// ContentType satisfies huma.ContentTypeFilter, so the response is labelled
// application/problem+json rather than application/json.
func (p *Problem) ContentType(string) string { return ContentType }
