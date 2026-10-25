// Package view is one screen of one resource, decided once by the server and painted
// by any shell: the same Page JSON drives a web screen, a phone's native views and a
// terminal. It is a pure value package — the third in the line kit/entity →
// kit/entity/display → kit/entity/present — that links no UI, no HTTP, no database and
// no module, performs no I/O, reads no clock but the one it is given, mutates no input
// and is safe for concurrent use.
//
// A Page never omits a member: the only nulls in it are field values, "" and [] mean
// none, and a shell that has to guess what an absent key meant is deriving again, which
// is the double derivation this type exists to end. PageVersion is the revision of that
// meaning and SupportedVersion says which revision this build reads; a page written at
// another is refused whole. The value domain it fixes, and the two differences from
// today's renderers that a shell must review, are [ADR 0023].
package view
