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
// nothing, imports no HTML and holds no sentence intended for a person.
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

import "errors"

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
