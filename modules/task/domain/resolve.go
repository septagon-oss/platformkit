// Package domain owns portable task decisions. It does not load tasks, authorize
// callers, sample a clock, persist changes or publish events.
package domain

import (
	"strings"

	"github.com/septagon-oss/platformkit/kit/fault"
)

// Lifecycle states retain the task entity's existing stored and wire values.
const (
	StatusOpen         = "open"
	StatusAcknowledged = "acknowledged"
	StatusInProgress   = "in_progress"
	StatusResolved     = "resolved"
	StatusClosed       = "closed"
)

// The two refusals this decision can reach, each a fault.Reason rather than a
// bare sentence: the verdict, the sentence this module is written in, and the
// entry of modules/task/messages that sentence is kept under. A decision is taken
// without a reader in mind, but the person who is refused has one, and the page
// that draws them asks this key for the sentence in their language. Nothing here
// knows that a page exists: the key is a name, and whoever renders owns the words
// it answers with.
var (
	ErrClosed = &fault.Reason{Kind: fault.ErrConflict, Key: "conflict.task.closed",
		Text: "a closed task cannot be resolved"}
	ErrDifferentResolution = &fault.Reason{Kind: fault.ErrConflict, Key: "conflict.task.different_resolution",
		Text: "this task is resolved with a different resolution"}
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
