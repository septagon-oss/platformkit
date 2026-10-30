// Package pkit explicitly composes an application from named modules and typed
// declarations. Build resolves and validates the complete plan before effects;
// it does not discover modules or supply undeclared dependencies. The plan can be
// read as sentences or a versioned description, containing declarations rather
// than tenant data or runtime observations. See
// docs/adr/0022-pkit-orchestrates-composition.md.
package pkit
