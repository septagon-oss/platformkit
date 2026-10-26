// Package secrets' shape, and the absences in it that are reasoned rather than
// left out.
//
// A read is traced and an envelope call is auditable at the key: every method
// opens one span (trace.go) — key, adapter, bytes wrapped, never a byte.
//
// The port emits no event: an audit trail is event-fed and commits its row beside
// the domain state the event describes, and a read has no domain state, so a
// "read" event would be a claim in a transaction of its own. The sealed event
// belongs to the command that seals and stores the ciphertext. It records no
// metric: the runtime's three numbers are slow, behind and refused. It caches
// nothing: a cached secret is a secret whose rotation nobody sees. It opens no
// transaction and takes no lock: a lookup happens before there is one to join,
// and README.md's Limits reasons about all four in full.
package secrets
