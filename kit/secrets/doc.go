// Package secrets' shape, and the four things it deliberately does not do.
//
// # Where the trail is, and why it is not an event
//
// The brief asks that every read be traced and that sealing and opening be audited
// at the key. Both are true here: every method opens one span (trace.go), and an
// envelope call's span names the key, the adapter and the number of bytes wrapped.
//
// What the port emits is no event and no audit row, and that is a reasoned absence
// rather than an omission. modules/audit's trail is fed by events, and its row is
// committed *beside* the domain state the event describes — that is what makes an
// audited fact and the write it describes disagree with nobody. A secret read has
// no domain state: there is no row whose transaction a "read" event could commit
// inside, so an event here would be a claim about something that already happened
// in a transaction of its own, which is exactly the pair that can disagree. And
// kit/ may not import a module, so the port could not write the trail itself even
// if the row made sense.
//
// So the event belongs where the transaction is: a command that seals a value and
// stores the ciphertext emits its own sealed event inside its own transaction,
// beside the row that holds the bytes, with the expected revision it rechecked. That
// is the caller's decision and its outbox, and the follow-up that would move a
// sealing module onto it is named in the README. No Observer or audit-hook field is
// declared on the port for it, because nothing in this delivery reads one.
//
// # Why no metric
//
// kit/telemetry Instruments are the runtime's three numbers — slow, behind, refused
// — with the reasoning that a number a dashboard cannot act on is a number nobody
// reads. "How many secrets were read" is not one of those three questions, and the
// per-call trail already answers it with the tenant and request attached. The day an
// operator asks for a rate, it is a fourth instrument in kit/telemetry and one call
// here, not a counter of this package's own.
//
// # Why no cache
//
// A cached secret is a secret whose rotation nobody sees: Version is the only signal
// the store offers, no caller in this tree caches on it, and the invalidation that
// would make a memo honest (rotate the secret, then move the generation) is a
// deployment act rather than a process one. So every Get reaches the store, and a
// caller with a real invalidation path may build a memo later on Version's back.
// kit/cache's rule about what may never be cached — sessions, grants, entitlements —
// is a different rule from this one: those are answers a database is the truth
// about, and this package's bytes are the truth wherever they came from.
//
// # Why no transaction, and no lock
//
// A read that joined the request's transaction would hold a secret's lifetime open
// across a commit, and a lookup happens before there is a transaction to open. There
// is no mutable state in this package, so there is nothing to serialise: the env and
// file stores read sources that cannot be partly written, and an HTTP client is
// goroutine-safe. A method takes the caller's context and no budget of its own —
// unlike a cache lookup, a secret read sits on the request's own critical path, so a
// caller that wants a bound passes its own context, and a cancellation is returned as
// the error it is and writes nothing.
//
// # Why the name grammar lives here
//
// Of and Shared refuse the name, in this package rather than in an adapter, so that
// no adapter can be wrong about it. The subset is the intersection of what a file
// name, a URL path segment and an environment variable can all spell, which is what
// lets env, file and OpenBao all address one name — and it is the one place traversal
// is refused, so an adapter that joins a name into a path has nothing to escape from.
package secrets
