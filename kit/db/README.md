# Database connection limits

`Open(ctx, url)` retains 16 open connections, four idle connections and a 30 minute
maximum lifetime. Use `DefaultPool`, change the required fields, then pass the
value to `OpenWithPool(ctx, url, pool)` for an independently configured pool.
Both constructors refuse a superuser or BYPASSRLS application role.

`Pool.Validate` rejects unbounded/negative open limits, idle limits outside the
open limit, and negative lifetimes. Explicit zero idle connections disables
reuse; explicit zero lifetime disables retirement. `Conn.Stats()` exposes
standard `database/sql` pool occupancy and cumulative wait counts/time, without
exposing connections or a way around the scoped transaction API.

The application maps optional `database.max_open_conns`, `max_idle_conns` and
`conn_max_lifetime` fields to these limits. Omission retains the defaults. It
validates them before migration or startup. Every app role requires at least two
open connections: HTTP handlers open detached transactions while retaining their
authentication transaction; jobs hold an advisory-lock connection during work.
This minimum does not prevent saturation by concurrent requests that each hold a
connection while waiting for another; independent `OpenWithPool` still allows one.
Budget database connections across all processes, including web/worker replicas,
migrations and maintenance. A larger pool does not establish greater throughput;
use the [tenant-work benchmark](../jobs/README.md) to measure a concrete workload.
