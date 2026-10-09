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

Reused: the person and every write to one (`modules/user`'s `Invite`,
`ConfirmAddress`, `SetRoles`, `SetRoles`' locked read, `Deactivate`), the tenant
transaction and its row-level security, the caller's credential and the ceiling it
carries, the permission guard on the route, the outbox and the audit trail that
subscribes to it, `crud`'s page bounds and its `GetForUpdate` row lock, the locale
catalogue, and `dbtest`'s real schema.

Added: the protocol door (the documents, the nineteen operations, and the two
extension rules the connectors' own guides force), the three tables of membership,
external identifier and group→role binding, the derivation that turns a membership
into a role, and five events.

Made reusable: `httpx.Router.At` (an address a module names without knowing the
prefix) and the response body that names its own media type, both of which came out
of this module's need and are the kernel's now — `kit/httpx`, not `modules/scim`.
Nothing in this directory is reusable: the fake the brief asked for is a follow-up,
named below.

## Services

`contracts.Service` is the module's own port over the protocol: discovery, the user
collection, the group collection, and the tenant's group→role mappings. Its
implementation lives in `internal/` and takes `modules/user/contracts.Service` as a
collaborator, along with two ports the composition implements.
`contracts.RoleDirectory` answers the question over wherever role definitions
actually live — this module may read neither auth's role table nor user's role
column, so "is this a role this tenant knows" arrives as a port rather than an
import. `contracts.Authority` answers the other question those same two rows
decide: whether the caller of a write holds a permission. Which roles a person
signs with is user's column and what those roles grant is auth's table, so the
joined question arrives as a port too, and the answer the composition gives it is
the answer the guard on this module's own route gives the same caller.
`apps/platformkit/scim.go` answers both, over the two auth methods it already holds.

### Interface

None of this module's own. It declares no screen and no `rest.Spec`: the manifest
passes `Nav: nil`, and `internal/` holds no `ui` package, because a machine door has
no face to render and rule 6 lets a module's `internal/ui` exist only where a page is
its own. `git grep -n 'Page(\|PageRaw\|rest.Spec\|Catalog' modules/scim` answers
nothing. What a person administers — which group brings which role — is two workspace
routes at `/api/v1/scim/group-mappings`, and the generated command surface for them
is a follow-up named in the delivery report, not a claim made here.

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
it meant to spare. A `PATCH` reads the attribute named in `path` and, as Okta sends
it, the attributes named inside a pathless `replace`'s value — so the two spellings
of "this person has left" both stop them signing in, and the same two spellings of
"this person is active" both let them back in. One member is selected by
`members[value eq "<id>"]`, which is how RFC 7644 and Microsoft's provisioning guide
send it; read as plain `members` it would answer the standard request by emptying the
group. The discovery documents answer at the addresses they name: a schema is read by
its urn at `/Schemas/{id}`, one path segment, because that is the address the
`ServiceProviderConfig` and every document's own `meta.location` point a client at,
and the two collections of them answer inside the same `ListResponse` envelope every
other list of this protocol uses, because a client decodes a SCIM list as one
envelope wherever it appears — including at the two addresses it reads first.

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
- Every mutation re-checks the tenant scope and the state it is about to change
  inside its own authoritative transaction, and holds the group's row lock while it
  decides, so a concurrent delete cannot commit underneath a membership write. A
  refused write changes nothing and emits nothing. What this service cannot hold is
  the *caller's expected revision*: it issues no ETag and keeps no version to compare
  one to, so the protocol door refuses any write that arrives carrying `If-Match` —
  with 412, before it opens a transaction — instead of performing, unconditionally,
  the write the caller asked to have refused. `TestConditionalUserWritesCannotOverwriteANewerPerson`
  is that refusal, over `PUT`, `PATCH` and `DELETE` on a person the caller's copy no
  longer speaks for. The reads and the tenant's own `group-mappings` route take no
  such header, for the reason `internal/routes.go` states: a read loses nothing, and
  no connector sends a condition to an address this platform named. Anyone who wants
  compare-and-set has to give the door a resource version first, and the product's
  share of that is its own.
- A group's lock is not enough for what a membership write leaves behind, so each
  person it affects is locked too — ascending by id, and *before* their roles are
  worked out. Two collections, two writers, one person: each holds a lock the other
  does not want, and a `want` computed before the person's row is held is a
  computation about a moment the other writer is spending. The last one to commit
  would restore the role the first one revoked, which is a revoked grant that
  outlives its revocation; `TestConcurrentGroupRemovalsRevokeBothRoles` holds an
  unrelated transaction on the person's row to keep the two writers genuinely
  simultaneous and asks what the person is left holding.
- `MapGroup` re-checks who is asking inside the transaction the binding is written
  in, and not only at the guard on its route — for both shapes of caller. One shape
  this module decides for itself: `Principal.Permissions` is the authority a
  credential carries, and a non-nil list is a ceiling, so a caller whose credential
  carries one and does not name `user:manage` is refused here whoever it is and
  however it reached the function, and a provisioning key cannot bind a group to a
  role by naming the command instead of the door. The other shape is a session, which
  carries no list of its own: its authority is two other modules' rows, so the
  command asks `contracts.Authority`, which the composition resolves over user's role
  column and auth's grant table, never more weakly than its route guard answers. A
  person with no roles, one whose roles do not grant it, one this tenant holds no
  active row for, and a write that names no caller at all are all refused — with no
  row, no write and no event. `TestMappingCommandRefusesAProvisionOnlyActor` holds
  the first shape and `TestMappingCommandRefusesASessionWithoutRoles` the second.
- No group write binds a group to a role. `role` is this platform's extension, not
  part of the core Group schema, and a credential handed to somebody else's identity
  provider that could state what a mirrored organisational unit grants could grant
  itself anything. `MapGroup` at `group-mappings`, under `user:manage`, is the one
  writer; a draft that names a role is refused where it arrives rather than dropped.

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

What happens to a person is the user module's event — `user.invited`,
`user.email_verified`, `user.roles_set`, `user.deactivated` — which is what lets a
subscriber see an identity created by a connector and an identity created by an
administrator as the same fact, and lets each grant and each revoke be audited.

The five facts no user event describes are this module's own, published in the
transaction that writes the row they are about: `scim.group_created`,
`scim.group_changed` (with `via` saying whether the list arrived whole or one
operation at a time), `scim.group_removed`, `scim.mapping_changed` — the last
carrying both role names, because after a withdrawal the table is the only place
that has stopped saying what a membership meant, and the trail is the only place
left that ever knew — and `scim.user_changed`, which names the attributes of a
person that no `user.` event describes and that no other module could: `displayName`,
which `modules/user` owns as a column and owns no command for, and `externalId`,
which is this module's own row. A write that restated what was already there emits
nothing, from any of the five. A mapping re-stated unchanged is the clearest case:
an event saying a decision was made when it was only repeated is a false line in the
one record a person reads to find out who decided what a role means.

The audit module subscribes to every declared event, so declaring these is what makes
a group write auditable at all — and `audit` is why the payloads carry identifiers
rather than content: a group's id and the name its provider spells it by, how many
members it now holds, and which role stopped being granted.

## Limits

What this door refuses, and what a caller has to do instead.

| The door refuses | Because | Instead |
| --- | --- | --- |
| a group document naming `role` | the mapping is the tenant's, not the provider's | bind it at `group-mappings` under `user:manage` |
| a `remove` on `members` with a selection other than `value eq "<id>"` | an operation that silently meant "every member" empties the group | name one member, or omit the path and mean it |
| a PATCH operation with no path whose value is not an object of attributes | guessing which attribute a bare value means | name the attributes, as `{"active":false}` does |
| `userName`, a `password`, an unknown schema urn, any group `filter` | one address per person, chosen by nobody but the person; a subset handed over as the whole directory | read the notes above |
| a `DELETE /Groups/{id}` that still has members | membership has one door | remove the members through it first |
| an `If-Match`, on any write, a create included | this service issues no resource version, so it has nothing to evaluate one against, and will not perform a conditional write as an unconditional one | send the write with no condition; `contracts.Service`'s note on concurrency |
| any of the above, once refused | nothing of it is kept | — |

The protocol's own conformance door (`contracts/scimtest`) is not here: the module is
exercised against a real Postgres, the real user module and the real router, which is
what the tests below run. A portable fake is a follow-up, named in the brief.

## Verification

Every case is `go test ./modules/scim/... ` against `dbtest.Schema(t, user.Migrations,
scim.Migrations)` — a real tenant transaction, the real `modules/user`, and the real
`httpx` router with its real grant chain — and `go test ./apps/platformkit -run SCIM`
asks the composed application the same questions through a token it minted. The
naming is the claim, so the list is the acceptance trace rather than a summary.

| The claim | The case |
| --- | --- |
| a provider vouching for an address is what makes a person able to sign in, and neither half sets a password | `TestTheProviderVouchingForAnAddressIsWhatMakesAPersonAbleToSignIn` |
| every attribute write is auditable in the transaction that wrote it, and a restatement writes nothing | `TestEveryUserAttributeWriteLeavesATransactionalAuditEvent`, `TestAPersonWriteThatChangedNothingLeavesNoAuditLine` |
| `PUT` and `PATCH` both honour `active`, in both directions | `TestReplacingAnInvitedUserHonoursTheActiveAttribute`, `TestConnectorDeactivationShapesStopThePerson` |
| the collection carries its role onto its members and takes it back off, through one member, one leaver and a concurrent delete | `TestAGroupCarriesItsRoleOntoItsMembersAndTakesItBackOff`, `TestFilteredMemberRemovalPreservesOtherMembers`, `TestMembershipCannotCommitBehindAConcurrentGroupDeletion`, `TestConcurrentGroupRemovalsRevokeBothRoles` |
| no group write, from no door, writes the tenant's binding — and the binding's own command refuses a caller whose credential or whose roles do not carry it | `TestNoGroupCommandWritesTheTenantsRoleMapping`, `TestProvisioningCannotChooseTheTenantRoleMapping`, `TestMappingCommandRefusesAProvisionOnlyActor`, `TestMappingCommandRefusesASessionWithoutRoles`, `TestAMappingWrittenByACredentialThatCarriesUserManage` |
| a refused write changes nothing, emits nothing and returns no row, including when the role directory is unreachable and when the caller's own condition names an older version | `TestWithdrawingAMappingRevokesTheRoleItGranted`, `TestAMappingToARoleNobodyDeclaredIsRefusedAndWritesNothing`, `TestRoleDirectoryFailureDoesNotCommitAMapping`, `TestConditionalUserWritesCannotOverwriteANewerPerson` |
| a tenant cannot see, change or be affected by another tenant's provisioning | `TestForeignTenantCannotMutateOrReadProvisionedState` |
| the last administrator survives every SCIM write, and the connector's deactivation is the one that stops them signing in | `TestSCIMRefusesTheLastAdministratorWithoutChangingState`, `TestConnectorDeactivationShapesStopThePerson` |
| the discovery documents describe the door that answered them, at addresses that answer, in the envelope a client decodes | `TestTheDiscoveryDocumentsAnswerForThemselves`, `TestEveryAdvertisedSchemaCanBeReadByItsIdentifier`, `TestDiscoveryCollectionsUseTheSCIMListEnvelope`, `TestTheSCIMDoorAnswersAtTheAddressTheProtocolNames` (in `apps/platformkit`) |
| an anonymous caller and a key scoped to the wrong door meet a refusal that wrote nothing | `TestProvisioningCannotChooseTheTenantRoleMapping`, and the kernel's own `kit/httpx` refusal cases |

Not verified by anything here, and not claimed: a live Entra or Okta tenant, the
browser and device journeys of a tenant that configured one, the localisation of a
refusal sentence into the tenant's language, and compare-and-set, which this service
does not implement and therefore refuses. What *is* verified is the refusal: a write
carrying a stale `If-Match` is answered 412 and changes nothing, which
`TestConditionalUserWritesCannotOverwriteANewerPerson` asks of `PUT`, `PATCH` and
`DELETE` on a person who has moved on since the caller read them. Whether a real
connector sends that header at all, and what it does with a 412, is a question for a
live tenant and is not answered here.
