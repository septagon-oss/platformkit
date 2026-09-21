# Audit module

`modules/audit` is the audit trail: it records every event the composed
modules emit and keeps the records for a configured period. The manifest
declares `module.SubscribeAll`, so a module that emits an event is audited by
having emitted it, wherever it sits in the composition. `audit:read` guards
`/api/v1/audit/events` and the admin entry at `/app/audit/events`; the
retention job removes expired rows tenant by tenant.

Compose it in [apps/platformkit/modules.go](../../apps/platformkit/modules.go)
with `audit.Deps{Tenants, RetentionDays}`; `config.example.yaml`'s
`audit.retention_days` supplies the period, zero means a year, and `Feature`
optionally puts the trail behind a plan feature. Consumers import
[contracts/](contracts/) for the record, events and permission, never
`internal/`. `make test TEST_PACKAGES=./modules/audit/...` needs the
development database.
