// Package module defines the manifest a capability exposes to the kernel: its
// permissions, events, subscriptions, jobs, migrations, routes and other declared
// contributions. Modules are constructed once with New(Deps) and composed through
// pkit, which validates the full plan before effects; see docs/adr/0022.
package module
