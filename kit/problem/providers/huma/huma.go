// Package huma adapts Huma errors to the shared RFC problem representation.
package huma

import (
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// NewError is the hook that makes huma speak problem details. Assign it once,
// where the API is built:
//
//	huma.NewError = problemhuma.NewError
//
// It is a plain function rather than an init() so that the wiring is visible at
// the call site, like every other wire in this repository.
//
// A 5xx never carries its message to the client: at that point the message is
// as likely to be a driver string as a sentence. The cause stays reachable
// through errors.Unwrap for the logger.
func NewError(status int, message string, errs ...error) huma.StatusError {
	if status < http.StatusInternalServerError {
		p := problem.New(status, message)
		for _, err := range errs {
			if field, ok := errors.AsType[interface {
				error
				huma.ErrorDetailer
			}](err); ok {
				detail := field.ErrorDetail()
				text := detail.Message
				if detail.Location != "" {
					text = detail.Location + ": " + text
				}
				// Values can contain a password or an entire credential-bearing body.
				p.Errors = append(p.Errors, text)
				continue
			}
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
	return problem.FromError(status, cause)
}
