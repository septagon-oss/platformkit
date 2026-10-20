// Package inference asks a model one question about text and at most one
// picture, and returns what it said.
//
// It is a port, not a module: no entity, no table, no route, no permission and
// no manifest. The two kernel parts it is built on — the counter that rations
// calls and the tracer that measures them — are reached by construction, a port
// the composition fills, rather than by composition of a module. A module that
// needs a model declares that need and receives an Engine; nothing in this
// package knows which module is asking, and nothing here knows what the picture
// is of.
//
// This is the inference seam of decisions 0087 and 0093. Machine translation
// through locale.Translator is its intended second consumer; the composition
// bridge is deferred, and no application composes it in this checkout.
//
// # The one call, and what it is not
//
// Chat sends one ask and returns one answer. There is no streaming, no
// resumable call, no embeddings, no tool calls and no routing between providers:
// chat parity does not imply embeddings parity, so a port that grew both would
// make every adapter answer for a wire it was never measured against. A caller
// that needs a document read chunks it here and asks more than once; the byte
// bounds below are what make that loop safe.
//
// # What never leaves the process
//
// No prompt, instruction, image byte or answer text reaches a span attribute, an
// event, a log line or an error string. The span carries zero events precisely
// because an exception event's message is a sentence a future wrapper is free to
// rewrite with the caller's text inside it, and "no event on this span" is a
// promise that survives other people's edits where "careful messages" does not.
// What a trace backend holds about a call is the model, the provider, the
// duration and the token counts the provider itself reported. Provider response
// model names enter telemetry only when they match the configured name; finish
// reasons are never quoted in errors.
//
// # Where a call may not be made
//
// A Chat never runs inside a database transaction. The counter's row is written
// in its own detached transaction (see kit/limit, and ADR 0010) because a request
// that rolls back still spent the call the provider served; a thirty-second model
// call held inside a caller's row-level-security transaction would hold that
// connection and its lock for the length of somebody else's inference. Resolve
// the tenant, commit what the caller is about to do, then ask.
//
// # Who may ask
//
// Not decided here: no permission name, no role, no actor check. The port checks
// three mechanical things — that an actor-resolved tenant is present, that the
// tenant's own allowance is unspent, and that this installation configured an
// endpoint at all. Authorization belongs to the owning module's command, inside
// its own authoritative transaction.
package inference
