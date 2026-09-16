# Changelog

## Unreleased

The kernel stops deciding what it does not own. `ui/document` and `ui/resource`
render a document and a resource's screens from values — no database, no router
— and `ui/page` and `ui/screens` stay as the adapters that read a request and
carry the kernel's rules; `kit/entity/display` owns how a value reads, with
`kit/rest` delegating. `kit/app` no longer imports an event provider: the
application supplies `app.Transports{Memory, JetStream}` and the kernel selects
by name, refusing at `New` when the selected name has no constructor.
`events.Memory()` is removed — call `memory.New()`. Each reference module ships
its own SQL under `modules/<name>/migrations` and adopts the history the
foundation applied for it, so an existing installation is re-owned by checksum
and nothing re-runs; `migrations/` is the kernel's schema alone.
`scripts/check_packages.sh` records every new boundary. `ADR 0013` explains why
`kit/module` stays a typed manifest.

The composition layer becomes values a second shell can call. `ui.Compose`
returns a `Sheet`; `ui/page` holds `Chrome`, `Request`, `View`, `Frame` and
`Navigation`, with `page.Serve` as the one adapter between a handler and the
router; `ui/screens` is the seven generated pages of a resource as pure
renderers plus `Mount`, and `screens.Describe` publishes the same knowledge as
JSON at `GET /api/v1/admin/resources` for a shell that is not a browser.
`modules/admin` is composition only. Controllers read the sign-in path off
`<html>` and name no route; the confirm dialog's inline handler, which the
content security policy blocked, is gone. Ceilings re-baselined; two packages
join the binary.

## v1.0.0

The extracted reference architecture replaces the 0.x CLI scaffolder that lived
at this module path (releases to v0.15.1). The 0.x line is kept reachable under
the `legacy-0.x` branch and its tags; nothing from it is imported here. What
v1.0.0 is: `ARCHITECTURE.md`. What it promises about size: `loc-budget.json`.
