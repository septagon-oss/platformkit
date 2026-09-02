package problem

import (
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// HumaError is the hook that makes huma speak problem details. Assign it once,
// where the API is built:
//
//	huma.NewError = problem.HumaError
//
// It is a plain function rather than an init() so that the wiring is visible at
// the call site, like every other wire in this repository.
//
// A 5xx never carries its message to the client: at that point the message is
// as likely to be a driver string as a sentence. The cause stays reachable
// through errors.Unwrap for the logger.
func HumaError(status int, message string, errs ...error) huma.StatusError {
	if status < http.StatusInternalServerError {
		p := New(status, message)
		for _, err := range errs {
			if err != nil {
				p.Errors = append(p.Errors, err.Error())
			}
		}
		return p
	}
	cause := errors.Join(errs...)
	if message != "" {
		cause = errors.Join(errors.New(message), cause)
	}
	return serverError(status, cause)
}
