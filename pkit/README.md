# pkit

pkit composes an application in sentences: an app is a name and the modules it
uses, and the modules wire themselves by the contracts they declare
([ADR 0022](../docs/adr/0022-pkit-orchestrates-composition.md)). Every method
records and `Build` answers. The engine rules that do not fit a package doc live
here.

## How a composition is answered

The wiring resolves when `Build` runs — not when the sentences are written, and
not once per request. The validate phase runs in a fixed order and every problem
comes back at once, each naming the method that caused it, before anything has an
effect:

| Phase | What it answers |
| --- | --- |
| P1 settings | an app with no name, a customisation set twice, an `AskForAccess` with one half missing, an environment nobody named, a `Choose` of a module `Use` never named |
| P2 resolve | duplicates, missing or ambiguous providers, cycles, phase violations, `FromDeployment` inputs, a simulated implementation outside development |
| P3 dry build | every module's build, its `Puts` counted, a contribution no composed module takes, and the kernel's manifest gates (`module.Expand`, `module.Validate`) over what the build produced |
| P4 ports | the three required ports have exactly one provider each, and every recorded role grants a permission a composed module defines and none that reaches the control plane |

A composition that does not resolve is refused rather than half-built, so a
running application has no wiring left to discover. The composition is still a
list somebody wrote down: nothing is found that `Use` does not name. A contract
is keyed by its Go type, and `reflect` is used for that type's identity and its
printed name only — never to discover, construct or call anything.

`Run` is `Build` plus whichever half the role names. `Host` adds the process
around an app: its configuration, the environment the composition is read in, the
role it runs, and `Host(app, Tenant(name, host), …)`, which says where an
application is reached without deciding which host is which tenant — that answer
stays the tenant module's rows, read per request. `Explain` writes what the
composition answers, so a committed composition file and a running application
cannot disagree.

## Free of both effects

`Build` is the last place a composition can be refused for free, and `Plan` is
that answer isolated: nothing in it opens a connection, migrates, writes, listens
or dials. What remains — the pool, the transport choice, and the gate that every
declared operation has an authorization — belongs to kit/app's effect phase,
because the third needs the open pool.

*Free* means free of both sides of the effect. An app refused above the
connection keeps no record of the attempt, so a corrected composition builds on
the same `App`; an app whose boot reached the pool keeps the lifecycle it handed
over. `App.release` tells the two apart and `app.RefusedBeforeEffects` answers
which side a refusal was answered on.

The engine registers routes four times: three dry registrations over an in-process
recorder (`app.Declarations`) and a fourth build of the API that would serve. They
are registration-time checks — an operation guarded by a permission no composed
module defines, an operation that publishes an event no module promised, a
workspace that mounts nothing, an address that names a prefix, and registrations
that did not mount the same surface — and none of them reads the database. Three
dry passes, not one: the second is what a callback behind an idempotent mount
guard needs, the third is what a callback that answers differently on its third
call needs. The fourth is judged against the first by the same comparison
(`app.registrationsAgree`) and is the last refusal the composition can be given
free of effects. A module's `Routes` callback that mounted differently between any
of those runs would otherwise be a composition that means two different things.

## One application per database

`claims.go` is the process's record of which application runs on which database.

`Server.Host` already refuses a process that hosts two applications and
`Server.Explain` refuses to write a composition file for one, but both answers are
read off one `Server` value, which is the whole of their scope: a second `Server`
value, or a bare `App.Build`, is a second boot the first never heard about, and
the two can name the same database. Two compositions over one database is the
arrangement kit/app's `Runtime.Work` says it does not support — `events.Relay`
claims any unpublished outbox row and stamps what it publishes, so one
composition can move another's event onto a transport nobody subscribes to, and
two schedulers over one database silence each other's same-named job through the
advisory lock.

So the claim is per process, keyed by database, and carries that application's
composition: a second boot with the held name and a different list of modules is a
different application wearing it. The event catalogue answers the other half —
keyed by app (`events.ClaimApp`), released by the `Close` of the runtime that
claimed the name — and refusing one composition per database is what keeps that
catalogue from being asked to paper over two compositions writing one outbox.

The claim is taken before the first effect and released by the lifecycle that took
it, so a process that holds nothing on a database claims it again freely and a
deployment can restart into a different composition once the live one is down.

What this check is **not**: a lock across processes, and not a reading of what the
database already holds. Two OS processes pointed at one database each carry their
own record, and one database spelled two ways is one database to Postgres and two
keys here. The cross-process, cross-spelling answer belongs to the change that
puts the app's name in every name two apps share and the check at every boundary;
the refusal says so, because an operator told "one application per database" while
running two databases is being told about the wrong thing.

## What a `Description` never carries

`Describe` is the resolved declarations and nothing else. Six things are
deliberately absent, each because this package does not own it:

- Not routes or mounted surfaces. A manifest registers its routes through a
  function rather than listing them, and kit/app mounts them; verifying what is
  mounted stays the consumer's. Adding a path key would be a fabrication.
- Not which module answers the kernel's three questions. Those are read off what
  each module *put*; this is a reading of the resolution and stops there.
- Not tenant hosts, and no tenant data of any kind. A host claim belongs to a
  process, and which host is which tenant stays the tenant module's rows.
- Not the built manifests. Permissions, emitted events and subscriptions are
  machine-readable through `Planned.Modules` and the AsyncAPI document; a second
  encoding would be a second owner of facts another package owns.
- Not observed runtime behaviour. It proves nothing about a boot, a migration, a
  transport or a served request: it never reaches the phase that opens one.
- Not a timestamp. No `generatedAt`, for the reason `Explain` gives for the file a
  client commits: a clock in the document makes every committed copy drift without
  the composition changing.

The one rule about a refusal: when the composition does not resolve, the document
carries schema, app, environment, `resolved:false` and problems, and no module,
contract, provider, implementation or role. A caller that ignores the error still
cannot read a composition out of it, and there is no second, softer graph behind
the first one.

## What runs during `Describe`

Every module's own build function runs, in build order, because what a module
reads and puts is observable only from inside its own build. The acceptance line
comes with it: nothing in this phase opens a pool, runs a migration, dials a
broker, listens on a port or starts a server, and a `Deployment` whose transports
and caches are all empty is described as readily as a full one.

What cannot be promised is stated rather than papered over: a build function is
the module's own code and this package cannot bound it, so this is a call into
your own composition, not an untrusted-code sandbox.

## Reused, Added, Made reusable

**Reused** — the ADRs this page cites rather than restates: ADR 0022 for what pkit
is, and kit/app for the engine. **Added** — this file, because pkit held no `.md`
at all and the engine's rules had nowhere to live except a package doc, which is
how they reached forty lines. **Made reusable** — a home for engine prose that a
later round can put rules into instead of growing a doc comment.
