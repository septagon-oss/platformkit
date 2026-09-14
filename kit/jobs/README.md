# Periodic tenant work

Use `PerTenant` for ordered, sequential callbacks. `PerTenantConcurrent(ctx, conn,
lister, workers, fn)` explicitly opts into concurrent callbacks through a fixed
worker pool. Audit the callback and its providers for concurrent use first.
Audit retention uses at most four workers, reduced to leave the scheduler's
advisory-lock connection available. Its real composed-job tests cover pools of
two, five and sixteen connections. Other existing tenant jobs remain sequential.
Do not pass an open or lazy transaction. Listing commits before callbacks start;
each callback opens its own tenant transactions with `db.Run`.

One tenant's failure leaves other work running. Errors retain tenant-list order
and support `errors.Is`; cancellation stops dispatch and waits for active callbacks.
Callbacks must honor their context. The lister still materializes all tenants;
this API does not provide streaming enumeration or global fairness between jobs.

The scheduler holds one pool connection for a nonparallel job's advisory lock.
Leave capacity for its transactions and other work, and multiply connection
limits by all replicas. `Job.Parallel` bypasses that cross-replica lock; it does
not control this helper's tenant concurrency.

Run `make load-test` with the same disposable PostgreSQL test URLs/ports used by
`make check`. It creates and removes isolated schemas and writes four rows in
four transactions per tenant across 64 tenants. The benchmark compares worker
limits 1/4 and pool limits 2/5/16, including the scheduled-job lock, and verifies
the committed update count and tenant isolation. Output reports tenant throughput,
pool waits and observed callback concurrency. It has no artificial delay and
does not measure HTTP, outbox delivery, cross-region latency, restart, restore or
production capacity. Preserve the Go version, database version, machine and
competing workload alongside results before comparing separate runs.
