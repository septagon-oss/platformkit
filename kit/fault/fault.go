// Package fault names the three failures a caller can act on. It imports the
// standard library and nothing else, which is the whole reason it exists: a
// package that has to refuse something — a module's value package, its
// events/, its domain/, or a contract whose service takes no transaction — can
// name the refusal without linking a transaction, a storage adapter or a web
// server. Wrap these. Do not re-declare them somewhere else, and do not reach
// them through kit/crud from a package that takes no transaction: that package
// is the gorm adapter, and importing it makes a value package transaction-aware
// for the sake of one error. kit/crud re-exports these very values rather than
// copies of them, so errors.Is matches whichever name a caller wrote.
//
// # The three, and everything else
//
// A caller distinguishes three failures: ErrNotFound, when there is no row this
// tenant may see; ErrInvalid, when what was sent cannot be accepted as written;
// ErrConflict, when the write contradicts data or business state that already
// exists. These three are not the whole of a refusal: the authorization errors
// kit/tenancy declares are answered ahead of these, a denied policy a 403 and
// an unreachable policy engine a 503, each with a code of its own, in the
// cases of kit/rest's Fault that sit above these. Anything the mapping does not
// recognise is an outage: the caller did nothing wrong and the platform could
// not answer, so it reaches a 500 with its cause in the log and nothing in the
// body. A fourth sentinel would be a new answer to what a caller can act on,
// which is a decision and not a convenience.
//
// # One value, two shapes
//
// These are the Go-level verdict, and there is exactly one of each. kit/rest
// turns them into 404, 422 and 409 for every handler, module-owned ones
// included. What a client is then shown — the problem document, or the refusal
// page and its table of sentences that kit/httpx and ui/page also call a
// "fault" — belongs to those owners and to
// docs/adr/0015-a-refusal-has-one-value-and-two-shapes.md. This package renders
// nothing and imports no HTML; its three values hold no sentence of their own.
//
// # The messages are a marker other packages parse, not decoration
//
// Each message keeps the "crud: " prefix it was given. That prefix names the
// package that owned these values before this one, and it is load-bearing past
// its own package rather than inert text: kit/rest derives the field message a
// client reads by trimming the literal "crud: invalid: " off a problem detail
// (FieldErrors, kit/rest/screens.go) and modules/admin asserts that a person is
// never shown that prefix. Two of the three also reach a client whole when
// nothing was wrapped around them: a bare ErrInvalid is a 422 whose detail
// reads "crud: invalid" and a bare ErrConflict a 409 whose detail reads "crud:
// conflict". The third does not — a 404 carries the sentence kit/rest holds
// for a row this tenant cannot see, so "crud: no such row" is what a log line
// says and never what a client is answered. Renaming a message is therefore a
// wire change with consumers on the other side of it, and the assertions that
// refuse the rename are kit/crud's TestSentinelsAreTheSharedValues, which pins
// each string against its literal, and kit/rest's mapping cases, which pin
// what a client is answered.
package fault

import (
	"errors"
	"fmt"
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
