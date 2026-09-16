# 0013: A manifest names the kernel's types

Status: accepted. `kit/module` stays a typed manifest; a declaration-only leaf
is deferred until a consumer that must not link the database asks for one.

## Problem

`kit/module.Module` is what a module tells the kernel: its permissions,
events, subscriptions, periodic jobs, navigation, SQL and routes. Three of
those fields carry functions over kernel types — `events.Subscription` holds a
handler over `db.Tx[db.Tenant]`, `jobs.Job` runs with a `*db.Conn`, `Routes`
takes an `*httpx.API` — and the migration adoption a module declares is a
`db.Adoption`. Importing `kit/module` therefore links `kit/events`, `kit/jobs`,
`kit/httpx` and `kit/db`, and through them the PostgreSQL driver and gorm.

The question was whether the manifest could be a leaf: declaration structs or
small interfaces owned by `kit/module`, with `kit/events`, `kit/jobs` and
`kit/httpx` adapting them at boot, so that a package reading manifests — a
navigation renderer, a permission catalogue, a documentation generator — links
no database.

## What a leaf would cost

A subscription is a function of a transaction, a job is a function of a
connection and a route registration is a function of the API. A leaf can carry
them only as values it cannot type — `any`, or interfaces whose methods name
types the leaf does not have — and the kernel would assert the concrete type
at boot. That moves the check that a handler has the right signature from the
compiler to `kit/app.New`, which is the check [ADR 0002](0002-explicit-wiring.md)
exists to keep in the compiler: the wiring graph is the argument list, and the
compiler checks it.

The alternative, duplicating `Tx`, `Conn` and the registration surface as
`kit/module` interfaces, would add a second definition of each kernel contract
that has to track the first, and every module in three repositories would
write its handlers against the copies. The consumers write these fields today
— the catalog declares 22 manifests and the client applications 8, and 20 of
them set `Subscriptions`, `Jobs` or `Routes` — and no alias can keep a field
whose type changed compiling.

## What a leaf would buy

Inside this repository the only importer of `kit/module` that is not the
kernel or a module is `ui/page`, for `NavEntry`; it reaches `kit/db` through
`kit/httpx` regardless, because a page is served in a request transaction.
Nothing here or in the consumers reads manifests without also serving them.
The database-free presentation cores this series adds — `ui/document`,
`ui/resource`, `kit/entity/display` — needed no manifest at all.

## Decision

`kit/module` keeps its fields typed by the packages that execute them. The
manifest is the one place a reviewer reads what a module does to the kernel,
and that reading is worth more when the compiler has already agreed with it.
`module.Validate` keeps checking the shape of what it can — names, permission
grammar, events, jobs' schedules, subscriptions against what is emitted, and
the migration adoption it forwards — and the kernel packages keep owning the
types.

## Adoption path

When a consumer appears that must read manifests without linking PostgreSQL,
extract the value-only declarations first: `Permission`, `NavEntry`, the event
names and the migration adoption are plain data and can move to a leaf that
`kit/module` re-exports by alias, with no change for existing manifests. The
executable fields stay where they are. If that consumer also needs the
executable fields, the cost above applies and is paid then, against a named
caller, as [ADR 0012](0012-independent-parts.md) requires.
