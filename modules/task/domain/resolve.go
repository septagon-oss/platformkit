// Package domain owns portable task decisions. It does not load tasks, authorize
// callers, sample a clock, persist changes or publish events.
package domain

import (
	"errors"
	"strings"
)

// Lifecycle states retain the task entity's existing stored and wire values.
const (
	StatusOpen         = "open"
	StatusAcknowledged = "acknowledged"
	StatusInProgress   = "in_progress"
	StatusResolved     = "resolved"
	StatusClosed       = "closed"
)

var (
	ErrClosed              = errors.New("a closed task cannot be resolved")
	ErrDifferentResolution = errors.New("this task is resolved with a different resolution")
)

// Resolution describes a proposed change. Changed, rather than Text, determines
// whether to write: an empty resolution is valid. The zero value changes nothing.
// A decision does not establish authorization, persistence or event delivery.
type Resolution struct {
	Text    string
	Changed bool
}

// Resolve decides whether requested text closes the task's loop. Equal text
// (ignoring surrounding whitespace), or empty text, retries a resolved task
// without changing its stored text or timestamp. Different text is a conflict;
// a closed task refuses every resolution request.
//
// Callers supply the current status and recorded text. For a change, they set
// StatusResolved, record Text and choose the resolution time when applying it.
// Other statuses resolve as before; entity validity and product-specific text
// requirements remain the caller's responsibility.
func Resolve(status, recorded, requested string) (Resolution, error) {
	requested = strings.TrimSpace(requested)
	if status == StatusClosed {
		return Resolution{}, ErrClosed
	}
	if status == StatusResolved {
		if requested == "" || strings.TrimSpace(recorded) == requested {
			return Resolution{}, nil
		}
		return Resolution{}, ErrDifferentResolution
	}
	return Resolution{Text: requested, Changed: true}, nil
}
