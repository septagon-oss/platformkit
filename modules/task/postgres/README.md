# PostgreSQL task resolution

This adapter uses the existing Task table, tenant isolation and transactional
outbox. Supply a `db.Conn` for a restricted application role after applying the
foundation migrations. Construction does not provision resources or start a
PlatformKit application. No broker connection is required to commit resolution;
an existing outbox relay handles subsequent event delivery.

```go
store, err := postgres.NewResolutionStore(conn)
if err != nil {
	return err
}
svc, err := resolution.New(store, policy, db.Now)
```

Use the resulting [service](../resolution/README.md) for standalone commands.
Its store calls `db.RunOwned`, which refuses an active transaction or pending
request instead of silently joining it. Success follows commit; a commit error
may require reading current state or retrying the same command to learn whether
the first attempt committed.

For product operations already holding a `db.Tx[db.Tenant]`, call
`LockResolution(ctx, tx, taskID, actor)`, check current authority against its
facts, then call `resolution.StageAuthorized`. Keep subsequent product writes,
authority rechecks and outbox records in that same transaction. Only the outer
caller commits or rolls back. The existing Task service uses this shared SQL
implementation while retaining its established policy and error contracts.

The explicit adapter actor replaces inherited user attribution. `PolicyUser`
requires a nonzero UUID. Named `PolicySystem` and anonymous `PolicyPublic` actors
write no user UUID; their policy identity is not a new field in the existing
outbox. Invalid actors are refused. Legacy Task calls retain their existing
context attribution, including the distinction between a principal and an
audit actor. The portable service itself reads neither identity nor transactions
from context.
