// Package inference asks a model one question about text and at most one
// picture, and returns what it said.
//
// It is a port, not a module: no entity, no table, no route, no permission and no
// manifest. A module that needs a model declares that need and receives an Engine;
// nothing here knows which module is asking, or what the picture is of. Chat is
// one ask and one answer: no streaming, no embeddings, no tool calls and no
// routing, because chat parity does not imply embeddings parity and a port that
// grew both would make every adapter answer for a wire it was never measured
// against.
//
// No prompt, instruction, image byte or answer text reaches a span attribute, an
// event, a log line or an error string. And a Chat never runs inside a database
// transaction: resolve the tenant, commit, then ask. README.md holds the rest.
package inference
