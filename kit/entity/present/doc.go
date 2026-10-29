// Package present is the one implementation of the presentation policy: what a
// resource, a field, a list and a command read as, decided from the author's
// hints and the schema, in the request's language.
//
// Resolve returns a Resolved, the value ui/screens prints as an entry's
// `resolved` object, so a title, a column order, an enum label or a sort query
// is computed once. The package knows the schema and a text seam and nothing
// else: no HTTP, no storage, no markup, no ui/* import. It reuses
// kit/entity/display for the word rules.
//
// Inside a Resolved every key is written. "" and [] mean "none"; a client never
// falls back to its own rule for them. A client that finds a policyVersion it
// supports uses the block for the whole entry, never a mix with its own rules.
package present
