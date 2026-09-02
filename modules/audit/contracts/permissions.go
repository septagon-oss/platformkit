package contracts

// The one permission an audit trail has, and the absence of a second is the
// decision: nothing writes through a route and nothing is managed, so reading
// what other people did is the whole of what this module can be asked for.
//
// The module.Permission value is in ../module.go with the manifest, which keeps
// kit/module out of every consumer's build graph — see modules/task.
const PermissionAuditRead = "audit:read"
