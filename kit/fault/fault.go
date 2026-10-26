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
	"fmt"
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

// Reason is a refusal's own sentence, carried as a value instead of as prose.
//
// A refusal has one value and two shapes (docs/adr/0015): the problem document a
// program reads, which says the same thing in every language because a program
// reads a code, and the sentence a person is shown. The second is the reader's
// language, and the writer of a refusal — a module's value package, which knows
// what it refused and knows no HTTP, no HTML and no reader — cannot know it. So a
// refusal that reaches a person carries two things besides the verdict: the source
// sentence its caller is written in, which is what Error answers and the log and the
// problem document print, and the key that sentence's own catalogue entry is kept
// under, which is what the layer rendering the refusal reads the reader's language
// from.
//
// The key belongs to whoever wrote the sentence, because the sentence does: a
// module refusing its own field owns both the words and the entry they sit under.
// A rendered page may answer the same verdict with copy of its own instead, and a
// refusal whose writer named no key is shown the source sentence in whatever
// language it was written in — the answer it always gave.
//
// Unwrap reports Kind, so errors.Is still matches the sentinel a caller tested for
// however the refusal is spelled, and every consumer of the three values is
// untouched by one being wrapped in words.
type Reason struct {
	// Kind is which of the three failures this is. It is one of the values above:
	// a refusal of any other kind is an outage, and an outage has no sentence for
	// a person to act on.
	Kind error
	// Key names this sentence in its writer's catalogue, in the form kit/locale
	// uses: the verdict, the module, the thing — "invalid.task.title". The empty
	// string says the writer ships no copy for this refusal.
	Key string
	// Text is the sentence in the writer's own source language, as a format
	// string for Args. It is the text a reader who speaks that language is shown,
	// and the fallback for one who does not while the catalogue ships nothing.
	Text string
	// Args are the values Text places. They are values, not sentences: a
	// translation puts them where its own sentence reads them.
	Args []any
}

// Error is the refusal in the language it was written in. It is what a log line,
// a problem document and a shell with no catalogue for this key all print.
func (r *Reason) Error() string {
	if len(r.Args) == 0 {
		return r.Text
	}
	return fmt.Sprintf(r.Text, r.Args...)
}

// Unwrap is the verdict underneath the sentence, which is why wrapping a refusal
// in words costs a caller nothing: errors.Is(err, ErrInvalid) matches whichever
// way the writer spelled it.
func (r *Reason) Unwrap() error { return r.Kind }

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

// Rule is one rule a submission broke, in the two shapes that same refusal has:
// Violation is what a program branches on and what the problem document answers,
// and Reason is the sentence the writer of the rule keeps in its own catalogue,
// which is what a person is shown. A rule whose writer ships no copy carries a
// Reason with an empty Key.
type Rule struct {
	Violation Violation
	Reason    Reason
}

// Keyed is a submission its own value refused for one or more rules the writer can
// name in a catalogue. errors.Is answers the verdict underneath the rules, and
// errors.As finds the Violations — the rules alone, spelled exactly as Invalid
// spells them — and the *Reason of the first rule, which is the sentence a refusal
// page shows. Error is the rules' own messages joined, so the Detail a client is
// answered did not move when the sentences were keyed.
type Keyed struct {
	Rules []Rule
}

// Refuse returns the rules as an error, or nil when the submission broke none: a
// submission that broke no rule is accepted. Each rule's Reason carries its own
// verdict, so Refuse needs none.
func Refuse(rules ...Rule) error {
	if len(rules) == 0 {
		return nil
	}
	return &Keyed{Rules: rules}
}

// Error is the refused sentences in the language they were written in, which is the
// text a log line, a problem document and a shell with no catalogue all print.
func (k *Keyed) Error() string { return k.violations().Error() }

// Unwrap is the rules and the first rule's sentence: a caller that branches on
// violations reads what Invalid answers, and a caller that shows a person one
// sentence reads a *Reason.
func (k *Keyed) Unwrap() []error {
	first := k.Rules[0].Reason
	return []error{k.violations(), &first}
}

// violations is what the rules look like to a caller that never wrote a sentence.
func (k *Keyed) violations() Violations {
	out := make(Violations, len(k.Rules))
	for i, rule := range k.Rules {
		out[i] = rule.Violation
	}
	return out
}
