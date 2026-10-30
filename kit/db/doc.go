// Package db provides typed database transactions, PostgreSQL row-level tenant
// scope, connection management and capability-owned migrations. Tenant access is
// set on each transaction; the database policies enforce isolation. See
// docs/adr/0011-migration-ownership.md for migration ownership. The [Run] and
// [RunSystem] examples show tenant and system transactions against PostgreSQL.
package db
