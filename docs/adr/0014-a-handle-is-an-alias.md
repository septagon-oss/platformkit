# 0014: A handle is an alias; the uuid stays the key

Status: accepted.

## Problem

A person in this database is a uuid. That is a good key and a bad thing to type,
say out loud, or print in a URL: `7f2c1a90-…` cannot be handed to a colleague, and
the column a human reads should not be a uuid.

The request was therefore to "use the username as the unique identifier instead of
the uuid". Taken literally that is a re-keying. At the time of writing 33 columns
across three repositories — `user_id`, `actor_id`, `member_id`, `author_id`,
`requester_id`, `assignee_id` — point at a user, and so does the subject of every
event and every row of the audit trail. Re-keying would rewrite all of it, and a
person who later changed their name would either rewrite history again or discover
that the trail no longer leads to them. The thing that makes a uuid a good key is
also the thing that makes it a bad name; the answer is to keep both, not to choose.

## Decision

`users.handle` is an optional text column. It is a name a person is *called*, and it
carries no referential weight whatever.

- **Per tenant.** Two tenants can each have a `sam`. This is what the `tenant_id`
  column already means everywhere else in this schema; an installation that made
  handles global would give one tenant the power to reserve a name from all the
  others, and would need a claim protocol nobody has designed.
- **Unique within the tenant, case-insensitively**, by a partial unique index —
  partial because unclaimed is the empty string, the way `display_name` already
  spells an absent piece of text, and a thousand unclaimed people must not become a
  thousand collisions on one empty string.
- **Renameable, and not releasable.** Renaming is the point of an alias. Releasing
  is refused: a name somebody can drop while still being answered to is a name two
  people can be addressed as.
- **Claimed by command**, `POST /api/v1/user/users/{id}/handle`, publishing
  `user.handle_set` with the name it *was* and the name it became. It is `Immutable`
  on the CRUD routes, so a profile PATCH cannot move it silently. This is the same
  argument the module already makes about `roles` and `status`, one column further along.
- **Refused to names that are not persons.** A short, published list in
  `contracts/user.go` — the platform, a function of it (`support`, `security`,
  `privacy`), a door of this API (`me`, `signin`, `api`), or a word that breaks a
  downstream reader (`null`, `true`). Reserved at claim time, not renamed later.
- **Not a login door.** `ByEmail` is the sign-in lookup and stays it. Signing in by
  handle is a separate decision with an enumeration surface, and the auth module owns
  what a failed attempt costs.

Case is folded in the entity's `Validate`, not in a trigger, so that `lower(handle)`
in the index and `Handle` in the struct agree about whether "Ada" and "ada" are one
name — the same reason `Email` is folded there.

## The alternatives we rejected

*Re-key on the handle.* Rejected above: it converts an ordinary human edit into a
migration, and silently corrupts the trail if the migration is not exhaustive.

*Keep the uuid visible.* Rejected: it works, and it makes every support call, every
mention and every URL a matter of reading hex aloud. That is what `external_id` in
another table would cost, too, minus the second lookup.

*A separate `handles` table with a history of who held what.* Rejected for now, and
the reason is the event: `user.handle_set` already carries `was` and `now`, so the
trail answers "who held this name, and when did it move" without a second copy of
the fact to keep in step. If a product needs to *resolve a historical handle to the
person who held it at a moment*, that is the point to add a table, with a query that
needs it on the table.

*Global uniqueness.* Rejected: it lets one tenant reserve names from another, which
is a shared mutable namespace and a coordination problem this schema deliberately
does not have.

## Consequences

A person can be found by a name a human typed, in their own tenant, without anybody
learning that they exist by trying that name in another tenant. A rename is one row
and one event: no backfill, no rewrite, and every past subject reference still
correct — which is the property to check if this is ever revisited. The reserved list
is a judgement call that now lives in code and can be argued about in one place, and
a name claimed badly can be claimed again badly by someone else only through the
same audited command. Nothing can sign in with a handle, so the enumeration question
is unopened rather than answered.

What a person is *called on screen* is this decision's other half, and it has a
mechanism now: `Handle` carries `ui:"present:person"`, `kit/entity` owns that
vocabulary and `kit/rest` refuses a name no screen renders, the same gate the write
axis already had. `ui/resource` composes the cell — a disc with the person's initial,
quiet to a screen reader, the name beside it — so a screen that renders the field
renders a person without the table, the module or a stylesheet being told. Unclaimed
reads as a dash, because a generic face would claim the screen knows whose is missing.
