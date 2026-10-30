// Package rest maps an entity's declared schema to HTTP routes, commands,
// permissions and events. A module declares a Spec in its manifest; mount checks
// its schema and presentation hints before routes are served. See
// ARCHITECTURE.md#entity-and-presentation-contracts for the presentation boundary.
// The [Spec.Mount] example registers a resource; [Command] shows a custom command.
package rest
