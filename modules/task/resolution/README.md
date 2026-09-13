# Durable task resolution

`resolution.Service` runs task resolution without a PlatformKit application,
HTTP server or database transaction in its command API. It needs an explicit
`AtomicStore`, `tenancy.Policy` and clock. Its dependencies are the shared pure
Task decision, portable tenancy contracts, UUIDs and the standard library.

```go
svc, err := resolution.New(store, policy, time.Now)
if err != nil {
	return err
}
result, err := svc.Resolve(ctx, resolution.Command{
	Tenant: tenant,
	Actor: tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: userID.String()},
	TaskID: taskID,
	Resolution: "Replaced the valve",
})
```

The host supplies authenticated identities; policy checks them against the
current locked Task on every call, including retries. Nil policy is refused.
The [PostgreSQL adapter](../postgres/README.md) supplies the existing Task storage
and outbox implementation; another store implements the same narrow effect port.

Success from `Resolve` means the store committed. An error returns no success
result, but a commit error can mean the acknowledgement was lost after commit.
Read current state or retry the same authorized command to reconcile that
outcome. Equal or empty text on an already resolved Task preserves its original
text and time and publishes nothing. Different text is a conflict, and a closed
Task refuses resolution. These rules remain owned by [domain](../domain/README.md).

`StageAuthorized` serves an existing caller-owned transaction. The caller must
hold the live lock and have checked current authority before invoking it. The
actor was bound by the storage adapter; this function does not rebind identity
or run policy. Its result is provisional until the caller commits. Later product
checks may still refuse the entire operation, as they do for Fleet incidents.

Stores must provide current tenant-qualified facts under a live lock, stage the
resolution and matching outbox event together, and release neither independently.
They call the callback once per attempt and do not retry a fragment of the outer
operation. Resolution timestamps retain UTC microsecond precision; returned
timestamp pointers do not alias storage. Database constraints and product text
limits remain the relevant adapter or product owner's responsibility.
