// Package fault names the three failures a caller can act on, and imports the
// standard library and nothing else: a value package, an events/ package or a
// contract whose service takes no transaction can then name the refusal without
// linking a transaction, a storage adapter or a web server. Wrap these rather than
// re-declaring them, and never reach them through kit/crud: its gorm adapter makes
// a value package transaction-aware for one error. kit/crud re-exports these same
// values, so errors.Is matches whichever name a caller wrote.
//
// ErrNotFound: no row this tenant may see. ErrInvalid: what was sent cannot be
// accepted as written. ErrConflict: the write contradicts state that already
// exists. Anything the mapping does not recognise is an outage — a 500, the cause
// in the log and nothing in the body — and kit/tenancy's authorization refusals
// are answered ahead of these. One value, two shapes: docs/adr/0015 names the two.
// The "crud: " prefix is parsed by kit/rest: renaming it is a wire change.
package fault

import (
	"errors"
	"strings"
)

// The three failures a caller distinguishes. What the mapping recognises past
// these is an outage, except the authorization refusals kit/tenancy declares,
// which are answered their own status above them.
var (
	// ErrNotFound is no such row in this tenant. Another tenant's row is not
	// found either, which is the only thing the API may say about it.
	ErrNotFound = errors.New("crud: no such row")
	// ErrInvalid is the entity's own Validate, or a query naming a field that
	// does not exist. The wrap names what was wrong; the sentinel alone does
	// not, and a caller reading a bare ErrInvalid has nothing to correct.
	ErrInvalid = errors.New("crud: invalid")
	// ErrConflict is a write that contradicts existing data or business state.
	// Whether the caller can fix it is the owning module's decision, not this
	// package's: a duplicate value is correctable, and the last one away is not.
	ErrConflict = errors.New("crud: conflict")
)

// Violation is one correctable refusal of a submission. Location is the wire
// name of the field it is about ("a.b" when nested, empty for the whole
// entity), Code a stable lower_snake token a client may branch or translate on
// (required, too_long, not_one_of, inconsistent, invalid, schema), and Message
// the English sentence a person reads. A message never echoes a submitted value
// that could be a secret.
type Violation struct {
	Location string `json:"location"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// Violations is the error a validator returns when it refused a submission for
// one or more reasons. It answers errors.Is(err, ErrInvalid) and is found with
// errors.As, so a wrapper that keeps the chain keeps the fields. Its text is the
// messages alone, because the sentinel's wrap supplies the prefix.
type Violations []Violation

// Error joins the messages with "; ".
func (v Violations) Error() string {
	messages := make([]string, len(v))
	for i, one := range v {
		messages[i] = one.Message
	}
	return strings.Join(messages, "; ")
}

// Is reports that Violations are an ErrInvalid.
func (v Violations) Is(target error) bool { return target == ErrInvalid }

// Invalid returns the violations as an error, or nil when there are none: a
// submission that broke no rule is accepted.
func Invalid(v ...Violation) error {
	if len(v) == 0 {
		return nil
	}
	return Violations(v)
}
