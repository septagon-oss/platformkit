# modules/scim — the door an identity provider provisions through

SCIM 2.0 over this platform's own people. The module owns two facts no other door
records: which collection a provider says a person belongs to, and which of its own
identifiers it calls them by. Everything else is a read or a write of what the
platform already holds — a person is created by `modules/user`'s `Invite`, activated
by its `ConfirmAddress`, given roles by its `SetRoles`, and stopped by its
`Deactivate`. There is no SQL in this module that writes the users table, and so no
second thing to be in disagreement with about who a person is.

A person provisioned here is the same row as a person invited from the workspace
screen, with the same events and the same last-administrator floor. That is the
module's whole design, and the reason a connector can be trusted with a door.

## Services

`contracts.Service` is the module's own port over the protocol: discovery, the user
collection, the group collection, and the tenant's group→role mappings. Its
implementation lives in `internal/` and takes `modules/user/contracts.Service` as a
collaborator, along with a `contracts.RoleDirectory` the composition implements over
wherever role definitions actually live — this module may read neither auth's role
table nor user's role column, so the question "is this a role this tenant knows"
arrives as a port rather than an import.

## Routes

Nineteen operations, mounted at the address the protocol names — `/scim/v2` — by
`httpx.Router.At`, which moves where the router stands and nothing else. The door
keeps the surface's credential chain, so a key scoped to `scim:provision` is the
only thing that opens it, and an anonymous request meets the same refusal the
workspace gives an anonymous request. There is no `/Bulk`: the protocol leaves it
optional, this service states `bulk.supported: false` in its own
`ServiceProviderConfig`, and the router has no such operation, so the document and
the door agree rather than promising a batch that would arrive as a thousand writes
in one transaction.

`/Bulk` aside, the collection is `GET`/`POST` with `PUT`/`PATCH`/`DELETE` by id, and
`GET` filtered by `userName eq "<address>"` — the one comparison this service makes,
because a filter it does not implement must refuse rather than answer unfiltered: a
connector handed everybody when it asked for a subset will go on to act on everybody
it meant to spare.

## Authorization

### Permissions

The manifest (`modules/scim/module.go`) declares one key, `scim:provision`, whose
words in the tenant's languages are in `modules/scim/messages`. It guards every
route at the protocol's door.

It deliberately guards no route at the workspace's door. The tenant's group→role
mapping is not a SCIM document — the protocol has no place to hold a role, and a
provider that could write it could grant itself anything — so it is an ordinary
workspace route guarded by the *user* module's own `user:read` and `user:manage`.
Reusing those is why the module declares no second name for one act: a tenant that
revokes `user:manage` loses the mapping screen on the same day it loses the ability
to rename a person, because both are the ability to say what a role means here.

### Object scope

Every route runs in the caller's tenant transaction, so tenancy is `db.Tx[db.Tenant]`
and row-level security rather than a predicate this module could forget: a group or a
mapping belongs to the tenant in its `tenant_id`, and a person belongs to the tenant
the row's RLS says it does. Nothing here takes a tenant id from a request.

The role a membership brings is not looked up from the group; it is looked up from
the *name* the provider spells, because that is the only fact connecting two systems
that never agreed on identifiers. The derivation rule is: a role that any mapping
names is the mapping's to grant and to take; a role no mapping names is untouched,
however it was acquired. That second half is what keeps an HR connector from
unmaking an administrator, and it is also why the rule is allowed to run
automatically at all.

### Duties the module enforces itself

- `userName` is `users.email`, this tenant's login lookup, and so belongs to
  `modules/user`: it is refused through every write rather than ignored, because
  silently accepting an immutable field is the gap that lets an identity be swapped
  while its id survives. Renaming is `user:manage` at the workspace door.
- A `password` on a user write is refused. SCIM's password operation has no analogue
  here — credentials are auth's, reset goes through the invitation and recovery
  flow — and accepting one silently would tell a connector it had set a secret.
- A `DELETE /Users/{id}` deactivates and keeps the address reserved, so
  re-provisioning the same person finds the same row rather than inviting a second
  one. A `DELETE /Groups/{id}` that still has members is refused, because membership
  has exactly one door and a group that lost its members on the way out would leave
  the roles those memberships brought with no record of why they went.
- An unknown schema on a resource is refused, not ignored: a payload carrying an
  extension this deployment does not store says something about that person that
  would otherwise be dropped without a trace.
- Every mutation re-checks the actor's grant, the tenant scope and the caller's
  expected revision inside its own authoritative transaction. A refused write
  changes nothing and emits nothing.

### Public faces

None. `git grep` finds no `httpx.Public()` in `modules/scim`. Discovery documents
are answered through the same guarded door as everything else: `serving a tenant's
directory structure to nobody` is a fact about an installation, and this one does not
make it free.

### The operator boundary

None. No permission here sets `Operator: true`, and nothing in `internal/` reads
`db.TxOperator`. The module's own tables hold one row per tenant's group and one per
group-to-role binding; a platform operator has no reason to write either.

### Provisioning

The module does not create roles and does not grant itself anything. A composition
grants `scim:provision` to whichever role holds the tenant's directory integration —
the reference application's wiring is in `apps/platformkit/scim.go` — and the
administrator role's wildcard satisfies it as it satisfies every key
(`docs/adr/0006-system-access-is-a-token.md`). Role *definitions* are read through
`contracts.RoleDirectory`, which the composition answers from auth's role list: a
mapping to a name that is not a role is refused with nothing written, and a
membership whose role vanished from beneath it leaves the person on their other
groups rather than silently making the binding mean something else.

## Events

The module emits nothing of its own. What happens to a person is the user module's
event — `user.invited`, `user.email_confirmed`, `user.roles_set` — which is what
lets a subscriber see an identity created by a connector and an identity created by
an administrator as the same fact, and lets each grant and each revoke be audited.
