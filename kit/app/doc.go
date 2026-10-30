// Package app assembles and runs a PlatformKit application from its configuration
// and module manifests. New validates the modules before Run opens connections,
// applies migrations or serves requests. Applications are composed through pkit;
// see docs/adr/0022-pkit-orchestrates-composition.md.
package app
