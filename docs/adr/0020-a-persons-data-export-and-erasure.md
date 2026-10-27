# 0020: A person's data: export and erasure

Status: built, with named debt, 2026-10-11. The kernel half arrived first — the contract, the
claim, and the boot that refuses a module whose tables say more than its manifest does. The two
operations are now written too: `modules/subject` holds the request row, the export walk, the
erasure command and the seven-day single-use link, and ten modules contribute their parts. So the
promises in §4 to §7 are about behaviour that exists and is tested, with two exceptions the table
at the end names rather than hides: a bundle carries file *metadata* and not file bytes, so the
`MaxBundleBytes` ceiling and its `LeftOut` manifest have nothing to bound, and no command walks the
declared `Text` columns to prove a person's words are gone — that proof is a case, not a residue
scanner, and the residual it leaves is stated in `modules/subject/README.md`. The table below says
which rows are pinned and which are debt; a claim in a `Verified:` paragraph that does not cite a
pinned row is not a claim.

## Problem

GDPR Articles 15, 17 and 20 give a person a copy of their data and its erasure, and every EU buyer
asks how both are done. Each answer in this repository today is one module's:

- the only per-person write in the tree is `file.Service.EraseSubject`
  ([`modules/file/contracts/file.go`](../../modules/file/contracts/file.go)), and it answers for
  `files`, `file_holds` and `file_erasures` alone;
- it **deletes**, because that module's trail proves bytes are *gone* — an application case
  (`apps/platformkit/erasure_audit_test.go:29`) proves exactly that scope: one subject's files, one
  digest, one trail;
- nothing anywhere exports one person: `grep -rn "func .*Export(" --include=*.go modules/` answers
  with OTLP telemetry exporters and the design-system `export.Export` and nothing else.

A person's rows sit in five other owners' tables, and house rule 3 — a module imports another's
`contracts/` and never its `internal/`, and no module reads another's rows — means no owner can
answer "this person, everywhere" even with goodwill. The rows a request has to reach, found by
walking every `CREATE TABLE` and `ADD COLUMN` in the tree — at `83b4967`, and again at `59d7f4d`
after the merge below brought `000044_file_image_dimensions` and `000045_file_uses`: the first adds
two `int` columns and changes nothing here, the second adds a table whose two uuid columns the walk
has to account for, and it is the reason the list is a walk rather than a snapshot:

| owner | tables naming a person | the columns |
| --- | --- | --- |
| user | `users` | `id` (the person), `email`, `display_name`, `handle`, `roles`, `password_hash`, `status` |
| auth | `sessions`, `password_tokens`, `verification_tokens`, `totp_factors`, `recovery_codes`, `api_tokens`, `first_factor_proofs`, `passkey_credentials`, `passkey_challenges` | `user_id` in each of the first six; `first_factor_proofs.user_id` is a primary key with **no** foreign key (`modules/auth/migrations/000033_first_factor_proofs.up.sql:69`); free text in `sessions.user_agent`, `sessions.ip`; a stored address in `verification_tokens.email`; a name a peer reads in `api_tokens.name`, whose `created_by` is a second person column |
| notification | `notifications`, `notification_deliveries` | `recipient_id`; free text in `title`, `body`, `link`; deliveries reach a person only through `notification_id` (`modules/notification/migrations/000027_notification_deliveries.up.sql:21`) |
| file | `files`, `file_holds`, `file_erasures`, `file_uses` | `uploader_id`, `placed_by`, `subject_id`, `actor`; free text in `file_holds.reason`, `file_erasures.reason`, `files.name`; `file_uses.record` is a polymorphic id (`module`+`entity` say whose row) that names this person's own record whenever the richtext field that displays the file is theirs, and `file_uses.file_id` names a file (`modules/file/migrations/000045_file_uses.up.sql:22-27`) |
| task | `tasks` | `assignee_id`; free text in `description`, `resolution` — somebody else's notes *about* this person |
| content | `contents` | `author_id` (deliberately no foreign key, `modules/content/migrations/000017_content.up.sql:30-32`); free text in `title`, `body` |
| change | `change_proposals` | `proposer`, `reviewer`, `subject_id` (an id of *another* module's row, not a person); free text in `comment`, and in `diff`, which is a JSON document kept in a `text` column |
| audit | `audit_events` | `actor`; `payload jsonb`, which carries an address verbatim (`modules/user/contracts/events.go:32`); `client_ip inet`, `request_id`, `traceparent` (`modules/audit/migrations/000035_audit_context.up.sql:35`) |
| kernel | `platformkit_outbox`, `platformkit_dead_letters` | `actor` (`migrations/000009_actor.up.sql:14`), and payloads that carry the same fields the event carried |

So the shape is **both** things at once, and pretending otherwise is how this task would end up as a
module that reads everybody's tables:

- a **contract every owner implements for its own rows**, declared in the manifest so the kernel can
  refuse a module that holds person columns and says nothing about them;
- one **orchestrating module** that composes the contributions, owns the two commands, the request
  rows, the bundle and the trail.

Reusing `file.EraseSubject` as the whole answer would be wrong in kind: it destroys what the audit
rule says to keep. Extending `modules/user` would make the people module the reader of every other
module's tables, which is the coupling the boundaries exist to prevent.

## Decision

### 1. One contract, in the kernel, with the brief's two methods

`kit/personaldata` — kernel, not a module, because the contract is what the manifest talks about and
the manifest is read before any module is built (the same reason `kit/module` names kernel types,
[ADR 0013](0013-manifests-name-kernel-types.md)):

```go
// Subject is the person a request is about, and the strings that must stop
// naming them anywhere in the application.
type Subject struct {
	ID          uuid.UUID
	Email       string // as it was written before erasure: the row may have copied it
	DisplayName string
	Handle      string
}

// Token is what a scrub leaves behind in free text in place of a name. It is
// derived from the id the audit rule requires to survive, so two mentions of one
// person still read as two mentions of the same person, and neither reads as a
// person.
func (s Subject) Token() string // "<erased 7f3c1a90>"

type Data interface {
	// Export answers for this module's rows about one person of one tenant.
	Export(ctx context.Context, tx db.Tx[db.Tenant], subject Subject) (json.RawMessage, error)

	// Erase removes what those rows say about that person. It runs inside the
	// caller's transaction: a half-erased person is not a person who is erased.
	Erase(ctx context.Context, tx db.Tx[db.Tenant], subject Subject) error
}
```

The `tx` is a `db.Tx[db.Tenant]` and not a bare tenant id — the same rule as `file.Service.EraseSubject`
and every command in the kernel, so a caller holding a transaction opened against the whole
installation does not compile here. RLS bounds what each contributor can see and change; the
orchestrator never asks a contributor to filter by hand.

`Subject` carries the strings and not only the id, and that is the whole reason the type exists. The
law asks that the *email and the name* stop being findable; the id it asks to keep. Only the
orchestrator knows both — it reads them from `usercontracts.Service.Get` before it asks anybody —
and only each owner knows which of its columns may have copied them. Handing the strings down is the
one way both halves are true at once. Handing down a bare `uuid.UUID` would leave every module to
guess, and a guess in a contract is a rule nobody can change.

Contributors hand over a `Part`, so the identity travels with the value:

```go
// Part is one module's answer, named by that module's own manifest string.
type Part struct {
	Module string
	Data   Data
}
```

A module contributes it with `pkit.Contributes[personaldata.Part]()` and needs nothing back; the
orchestrator takes `pkit.Needs[[]personaldata.Part]()`. The resolver already builds a consumer after
every contributor (`pkit/declare.go:80-84`), which is the ordering this needs and the reason the
orchestrator does not say `After(Everything)` — that declaration also refuses anything the
orchestrator provides or contributes (`pkit/declare.go:91-96`), and the orchestrator provides a
service the admin screen reads.

### 2. The manifest says, column by column, what a module holds

```go
// Declaration is a module's claim about the person columns in the SQL it owns.
type Declaration struct {
	Tables []Table
	// None is why no table this module creates names a person. Exactly one of
	// Tables and None is set; both is a contradiction and neither is silence.
	None string
}

type Table struct {
	Name    string
	Person  string   // the column holding this person's own row id
	About   []string // other columns naming a person: author, actor, created_by
	Text    []string // text and jsonb columns an identifier may have been copied into
	NotPerson []string // uuid columns naming a row and not a person: file_id, event_id
}
```

`Module.PersonalData Declaration` is the field. The kernel reads it three ways, which is what makes
it a declaration and not documentation: it is refused against the module's own migration bytes (§3),
it bounds what the per-person erasure re-reads to prove it worked (§4), and it is the list the
export bundle reports against (§5).

`id` and `tenant_id` are exempt from the claim by the kernel's own conventions: every table in this
repository has both, and `tenant_id` is the column `db.Tenant` already defines. Nothing else is
exempt — which has a consequence worth writing down before anybody meets it as a surprise:
**a module whose tables carry a uuid at all cannot declare `none`, however obviously that uuid names
a plan or a file.** It declares the table and files the column in `NotPerson`. The alternative —
accepting a sentence in place of the list — is the hole every future module would fall through the
day it adds a column whose name does not answer its own question.

### 3. What the boot refuses

Each of these is a `Validate`-time refusal in `kit/module`, on the manifest and on the module's own
migration files — bytes, and not a database, so a deployment that never boots a schema still gets
it. The claim is asked of a module that **has** SQL and of nobody else: a module with no tables of
its own creates no person column by construction, and asking `modules/admin` for a sentence about
nothing would teach every future module that the field is a formality you fill in to be quiet.

| id | refusal | state at this revision |
| --- | --- | --- |
| P1 | a module with SQL declares neither `Tables` nor a `None` reason | **in** — `kit/module/personal_data_test.go`; the refusal the field exists for |
| P2 | a `None` whose reason is empty or whitespace | **in** — refused wherever it appears, including beside a table list |
| P3 | a table the module's own SQL never creates or alters | **in** — a module may not name another module's table here |
| P4 | a column named in `Tables` that the SQL does not carry, or carries with the wrong type | **in** — a `Person` on a `text` column, and a `Text` on a `uuid`, which would scrub nothing and report success |
| P5 | a `uuid` column in a table the module creates that appears in none of `Person`, `About`, `NotPerson` | **in** — the completeness rule; see why below |
| P6 | a `CREATE TABLE` the reader cannot read, or an `ADD COLUMN` it cannot attach to one | **in** — a file it cannot fully read is an error out of `ReadMigrations`, so a column nobody saw is never a person column nobody had to declare |
| P7 | a module that declares `Tables` and contributes no `Part`, or contributes one whose `Module` is not its manifest name, or two with one name | **not yet** — it arrives with the first `Export`/`Erase`, because until a module implements the contract every declaration in the tree would fail it |
| P8 | a `Part` whose exported part names a table outside its own declaration | **not yet** — same reason: it reads bytes no `Export` produces yet |

P5 needs no vocabulary of likely person-column names, and the kernel deliberately carries none. A
constant like `{"user_id","actor","author_id",…}` would be both over-broad — it names columns the
kernel has no business knowing — and under-broad, stale the day a module adds a differently-named
one, and it is the kind of specialist fact house rule 6 keeps out of a shared module. P5 gets the
same coverage from the module's own SQL, which is the only source that cannot go stale.

What P5 cannot do is decide which **text** columns carry an identifier: a text column names nobody
until somebody writes an address into it, so `Text` is the module's own claim and a column left out
of it is caught by the erasure's re-read (§4) rather than at boot. Saying that plainly is the honest
version of a rule that would otherwise read as complete.

### 4. Erasure anonymises, and the declaration verifies it

The brief's rule, and the reason a per-person command is not a `DELETE`:

- the person's **id survives** in every row that referenced it, so the audit trail keeps its shape and
  "what happened to this row" keeps answering;
- the identifying **values go**: `users.email` becomes `erased-<id>@erased.invalid` (unique per
  tenant, so `users_tenant_email`'s partial unique index still holds and the real address is released
  for reuse; `.invalid` is reserved by RFC 2606 and can never be somebody's mailbox),
  `display_name` becomes `''`, `handle` becomes `NULL` (the index in
  `modules/user/migrations/000025_handle.up.sql:27-33` excludes NULL, and the `users_handle_form`
  CHECK only constrains a non-NULL value), `password_hash` becomes `NULL`, `roles` becomes `'{}'`;
- every column a declaration lists as `Text` is **scrubbed**: each occurrence of the email, the
  display name or the handle is replaced by `Subject.Token()`. The match is case-insensitive and
  whole-word, and which rows it may reach splits by where the string came from, because the three
  are not three spellings of one thing. An address and a display name are what a person is called,
  so they are found anywhere in the tenant's text: the row that *mentions* a person need not carry
  their id. A handle is drawn from the same characters as the language — `users_handle_form` accepts
  `will`, and "the deploy will run on Monday" is a colleague's sentence about a deploy — so a handle
  is taken for the person where it is written as one, an `@`-mention, and as a bare word only in a
  row that carries the person's own id. `kit/personaldata/scrub.go` owns both rules, and the
  verification below asks them back. This is what reaches `tasks.description`
  and `.resolution` (somebody else's note about the person), `contents.title` and `.body`,
  `change_proposals.comment` and the JSON in `.diff`, `notifications.title`/`.body`/`.link`,
  `file_holds.reason`, and the `payload jsonb` of `audit_events`, where an address sits verbatim
  today;
- **credentials are removed, not anonymised**: `password_tokens`, `verification_tokens`,
  `totp_factors`, `recovery_codes`, `first_factor_proofs`, `api_tokens`, `passkey_credentials` and
  `passkey_challenges` rows are deleted. A secret that outlives its person is a door. A passkey is the
  same door with no secret in it: the stored row is what an assertion is checked against, so deleting
  the row is the only way a public key that cannot be reset, revoked or emailed stops opening
  something — and its name is exported, because "laptop" and "phone" are the person's own words for
  their own credentials. `sessions` are ended, and their `user_agent`/`ip` scrubbed
  where the row is kept as history;
- **a person the directory hides is still held**: the lookup an export and an erasure make is the
  owner's `user.Service.Retained`, the row without `deleted_at IS NULL`. Soft deletion hides a row
  from a screen and from a sign-in; it does not forget the address, the name or the handle in it, so
  a command that could not reach that row would answer "erased" about a table that still says who
  the person is. The ordinary directory's filter stays exactly where it was — a hidden row signs
  nobody in and is not findable by `Get` — and the tenant bound of both reads is row-level security's;
- audit rows survive, anonymised: `actor` keeps the id, `payload` is scrubbed, `client_ip` becomes
  NULL. An address is personal data; `request_id` and `traceparent` are opaque and stay, because
  removing them would break the trail's own promise ("every row this trace touched") for no
  data-protection gain;
- erasure **refuses the person who is the last one who can still sign in**, by the floor that already
  exists (`user.Service.RefuseLastAdministrator`, `modules/user/internal/administration.go:96-105`,
  and `modules/user/contracts/administration.go:273` for what "reachable" counts). House rule 8:
  refuse the write that takes the last one away. The refusal writes nothing, emits nothing and
  returns no stale row;
- the command **verifies its own result inside its own transaction**: after every contributor's
  `Erase`, it re-reads every declared `Text` column and the `users` identity columns, and if any of
  them still holds the email, the display name or the handle, the whole transaction rolls back and the
  command returns the list of columns that lied. This is why `Text` is in the declaration and not in a
  comment: a rule nobody checks is a rule that quietly stops holding, and an erasure that "succeeded"
  over a row that still names the person is a certificate that lied — the same failure
  [ADR 0019](0019-the-tenant-is-in-the-object-name.md) refuses for a storage delete that missed a copy;
- a column the table holds **unique** gives up its **whole value** rather than the matched span, and
  takes the empty string (`personaldata.Blank`, which asks the same rows `Scrub` asks and rewrites
  none of them). The placeholder is one per person, and the rows of one tenant can carry one
  identifier in two spellings — "Jo.Jones@Example.COM" for one person and "jo.jones@example.com"
  for another, both of which a provisioning door accepts — so a case-insensitive rewrite makes two
  rows the same string and the unique index refuses the write. The person would then be unable to
  be erased because of a difference in case nobody can see, over data the module's own commands
  wrote. An identifier with a person's address replaced inside it is not a string anything will
  send again, so nothing that still worked is given up; the empty value has to be the value the
  column's own CHECK and partial index allow, which is why the caller names the column rather than
  the kernel guessing at it.

What is **not** scrubbed, and says so: `audit_events.name` (an event name is a contract, not a
person), and any row of another person's whose text does not contain one of the three strings. The
scrub is exact string replacement, not inference; a message that identifies a person by describing
them is not findable by any mechanical rule, and an erasure record that claimed to have caught it
would be overpromising. The product's share — a legal-hold class of row an erasure must not scrub —
is §*Limits*.

### 5. Export is a job whose output is a file

`modules/subject` is a new kernel module — the data subject's two doors, and the rows about the
requests. The name is not new vocabulary: `subject` is what `file.Service.EraseSubject`,
`file_erasures.subject_id` and `change_proposals.subject_id` already call this person. It is a
platform capability that every deployment composes, not an app's own module.

- `POST` an export request (the person's id, a reason) → one `subject_requests` row in state
  `requested` plus one outbox event, committed together;
- the worker walks every `Part`, in module-name order, each `Export` inside one tenant transaction,
  and zips: `manifest.json` and one `<module>.json` per part. Entries are stored sorted by name with a
  zero modified time, so two exports of the same rows are the same bytes and a bundle can be digested
  and compared;
- `manifest.json` names the tenant, the person id, the UTC instant, every module asked and which
  tables each answered for, the sha256 of each part, and **what it left out**: files whose bytes did
  not fit the ceiling (§below) are named there, never silently absent;
- the bundle is stored through `file.Service.Upload`, private, owned by the requester, held
  (`file.Service.Retain`) against the retention sweep for as long as the link lives;
- the person is told, and the notification rule of the module that owns notifications applies:
  a secret does not sit in a row (`modules/notification/contracts/notification.go:117-126`), so the
  in-app notification says *an export is ready* and points at the request, and the minted link goes in
  the mail and nowhere else;
- `Export` publishes nothing and writes nothing: it is a read. A failure of one contributor fails the
  job and leaves the request in `running` with its error, and the retry redoes the whole bundle —
  a partial bundle that reads as a whole one is the failure this design cannot afford.

A file's **bytes** are attached to the bundle while their total fits `MaxBundleBytes` (64 MiB at
first, a config the deployment raises); above it the file module exports the metadata — name, content
type, size, digest, uploaded-at — and the manifest names each file whose bytes it did not attach, so
the answer to "what is missing" is in the bundle. The alternative, an unbounded zip of a tenant's
object store, fails in a different way and much later: in production, mid-request.

### 6. The seven-day link is the export's own

`file.Grant`'s ceiling is `MaxGrantExpiry = 24 * time.Hour`
([`modules/file/contracts/file.go`](../../modules/file/contracts/file.go)), and its own comment says
the module's ceiling is the one that binds, inside the seven days an S3 signature may live. Raising
that constant for one command's link would widen the door every upload in the product walks through,
for a reason that is one command's. So `modules/subject` mints its own:

`subject_grants`: `token_hash`, `request_id`, `expires_at`, `redeemed_at` — one row per minted link,
the token stored hashed by the same rule sessions and password tokens follow
(`modules/auth/migrations/000013_session_hash.up.sql`, `000014_password_tokens.up.sql`), seven days,
single-use, on the public surface, and it streams the bundle out of the file module. An expired or
redeemed token answers the same refusal, and neither says which.

### 7. Who may ask

| door | guard | notes |
| --- | --- | --- |
| export of myself | the person's own session, `/me`-shaped, no permission | Article 15 is the person's right and cannot require a grant a person might not hold |
| export of anybody | `subject:export` | the operator's door, and the one the admin screen uses |
| erasure of anybody | `subject:erase`, plus `confirm` | `confirm` is the person's email, repeated, by the shape of `tenant.Delete.Confirm` (`modules/tenant/contracts/tenant.go:228-235`): not a secret, the one-keystroke refusal that stops a mis-clicked POST |
| the bundle link | the minted token | nobody's permission is consulted at 3 a.m. by a person on a phone with an expired link, and they get the same refusal as a wrong token |

Every command rechecks the actor's grant, the tenant scope and the row's expected revision inside its
own authoritative transaction, and an erasure is irreversible and so publishes `subject.erased` to the
outbox in the same commit as the state it changed. "Every command" includes the two that are not doors
— the read and the irreversible write — and the requester-only half of the read and the mint: what they
compare is the identity the transaction carries, never the id the caller supplied, because a caller
that could name the requester could name anybody. A route that guarded its door is not a command that
may assume it was reached through that door, since another module composes these commands with no route
in front of them at all. A mutation that is refused asks this before it reads the row it might change,
so it writes nothing, emits nothing and hands back no stale row. A second export of an unchanged person writes a new
request row and a new bundle (it is a different moment in time and the person may have been told
already); a second erasure answers the receipt of the first, and does not scrub twice.

### 8. The inventory, and what each owner does

Implement here, as the brief names: **user, auth, notification, file, task**. The acceptance line —
"erasing leaves no email or name of theirs in any table" — is what pulls in three more: **audit,
content, change** hold the person's identifiers in `payload`, `body`/`title` and `comment`/`diff`, so
a delivery that declared them `none` would fail that line. They implement, and this record says why
the brief's list is not the whole list.

| module | tables declared | Export answers | Erase does |
| --- | --- | --- | --- |
| user | `users` (Person `id`; Text none — the values *are* the columns) | email, display name, handle, status, roles, created/updated/deleted, **never** `password_hash` | §4's replacements; refuses as the last reachable administrator |
| auth | `sessions`, `password_tokens`, `verification_tokens`, `totp_factors`, `recovery_codes`, `api_tokens`, `first_factor_proofs`, `passkey_credentials`, `passkey_challenges` | sessions (device, address, times), tokens by *name and time only* — never a hash or a secret, factors by kind, a passkey by the name its owner gave it and never its credential id or its public key | ends sessions and deletes every credential row; scrubs `sessions.user_agent`/`.ip` on what stays as history |
| notification | `notifications`, `notification_deliveries` | recipient, title, body, link, read-at, and the delivery ledger (channel, outcome, reason) | anonymises `title`/`body`/`.link`; keeps the row, because `notification_deliveries` reaches a person only through it and deleting it would silently orphan a ledger that has an index and a CHECK |
| file | `files`, `file_holds`, `file_erasures`, `file_uses` (About `record`, NotPerson `file_id`) | metadata always, bytes under the ceiling; holds with their reasons; the erasure certificates that name this person as subject or actor; the uses that read this person's own records, which say which field of which record displayed which file | `uploader_id`/`placed_by`/`subject_id`/`actor` keep their ids, `files.name`, `file_holds.reason` and `file_erasures.reason` are scrubbed, and `file_uses` rows whose `record` is this person's are deleted with the record they name — 000045 rewrites that set hard, so a row outliving the record it points at is a use that ended. **The person's own bytes are not deleted by an erasure**: `EraseSubject` answers a *retention* question with a certificate, and Article 17 asks for a copy and for the identifying record; the two commands stay distinct and the request names which one ran |
| task | `tasks` | title, description, status, priority, source, due/SLA, resolution of tasks assigned to them | keeps the row (it is somebody else's work record), keeps `assignee_id`, scrubs `description` and `resolution` |
| content | `contents` | slug, title, body, kind, status, published-at of what they authored | keeps the row and `author_id`, scrubs `title` and `body` — a published page that silently disappeared is a lie about the tenant's own site |
| change | `change_proposals` | proposals they proposed or reviewed, with the diff | keeps `proposer`/`reviewer` ids, scrubs `comment` and the strings inside the `diff` document — the diff is `text` holding JSON, so the scrub reads it as JSON and writes it back as JSON |
| audit | `audit_events` | the person's trail: name, occurred_at, payload, request_id, client_ip | keeps `actor` and `name`, scrubs the strings inside `payload`, `client_ip` → NULL |
| scim | `scim_group_members` (About `user_id`, NotPerson `group_id`), `scim_group_mappings` (NotPerson `group_id`), `scim_user_external` (About `user_id`, Text `external_id`) | the identifier the provisioning client calls them by, whether this door is the one that stopped them signing in, and which of the provider's collections they belong to | keeps the row and `user_id`; empties `external_id` over the tenant — the column holds the string the client sent, a directory that keys people by mail sends an address, and the column is unique while it holds anything, so §4's rule for such a column is the empty value rather than the placeholder |
| kernel | `platformkit_outbox`, `platformkit_dead_letters` | the rows this person's requests caused that are still held, and any dead letter naming them | scrubs `actor`'s payloads and leaves `actor` — the same rule as the trail. Supplied by `modules/subject`, which is the only module that may import the kernel's own tables, and it says so in the manifest rather than in a comment |
| billing | `billing_subscriptions` (`NotPerson` `plan_id`) | nothing — no row of this module is a person's | nothing; **it cannot declare `none`**, because `plan_id` is a uuid and the completeness rule admits no exception for a name that answers its own question |
| site | `site_settings` (`NotPerson` `logo_file_id`) | nothing | nothing, and the same rule applies to `logo_file_id` |
| tenant, admin, web | not asked — **they own no SQL**: `modules/tenant`'s tables are the kernel's `migrations/`, `modules/admin` generates screens, `modules/web` serves the public surface | — | "the organisation is the data subject where a tenant row is concerned: `tenants.name` is a company's name, `tenant_hosts.host` is a domain, `tenant_locales` is a language row" — the reason lives in this record, because §3 asks nothing of a module that creates no table |

The scim row postdates this record: the module arrived later (`f7762a0`), and the walk above predated
it. It belongs here for §4's reason and not because anybody asked — `external_id` is a `text` column
whose value is whatever the provisioning client sent, so the person's own address is in this tree's SQL
by a route no column name predicts, and the completeness rule refuses a module that files such a column
as nothing.

### 9. What the new module owes the kernel's conventions

`modules/subject` is a module like any other: `contracts/` + `internal/` + `module.go`;
`personaldata.Part` contributed, `filecontracts.Service` and `notificationcontracts.Service` needed;
`subject:export`, `subject:erase` in `Permissions`, `subject-officer` in `Roles`; one `rest.Spec` for
the request rows and no Spec for the grant (a Spec is five routes and three of them write — the same
reason `modules/audit` mounts none); one `jobs.Job` for the export walk, per tenant through
`jobs.PerTenant` and a `jobs.TenantLister`, as `modules/task`'s `SLASweep` does
(`modules/task/module.go:144`, `modules/task/internal/sla.go:29-37`); migrations owner `subject`,
first version its own, `RulesFrom: 0`, `ENABLE`+`FORCE ROW LEVEL SECURITY` and
`platformkit_tenant_match` on both tables, like every table in the tree. Screens use the foundation's
layers, and `internal/ui` renders HTML only for the pages that are its own.

## Consequences this has to pay for

**The refusal is boot-time, so it is also a change to every module that owns SQL.** Ten kernel
modules gained a `PersonalData` claim in the same change as the field — eight naming person columns,
two (`billing`, `site`) filing the uuid columns they hold as rows and not people — or the reference
application stopped starting. That is the point of the field. Three more (`tenant`, `admin`, `web`)
own no SQL and are not asked; their reasons are the sentence in §8 rather than a line of Go that
nothing could check.

**A `Text` column is a promise that a scrub runs there.** Adding a text column to a declared table
without declaring it makes the *erasure* fail (P4 checks the declaration against the SQL, and §4's
verification reads the declared columns), so the drift surfaces as a red command rather than as a
person's email surviving in a note.

**Erasure becomes the slowest command in the kernel.** It touches eight owners, then re-reads every
declared text column to prove it worked. It is asynchronous by nature and should be run as such where
a tenant is large; the state machine (`requested → running → erased|failed`) is the same one export
uses, and a `failed` erasure is a request that wrote nothing and says which module said no.

**Two erasures of one person are one erasure.** `subject_requests` is unique on
`(tenant_id, subject_id, kind)` for the irreversible kind, so a redelivery answers the first receipt —
`file_erasures`' unique `(tenant_id, file_id, cause)` is the precedent for that shape of idempotency.

**A person already erased is not an error.** Like `file.EraseSubject`'s rule, an erasure of nothing is
an answer rather than a thing done: zero counts, no event, no audit row.

**The bundle is the most sensitive object in the store.** It is one zip containing one person's rows
from nine owners: private, retained-by-hold while the link lives, never listed in a public surface,
and never exported *about* an operator by another operator without that operator's own audit row.

## Limits — what this record decides, and what it leaves

The module's share, decided here so an implementation does not have to: the contract's two signatures
and the `Subject` type; the declaration's four column sets and the eight refusals; the anonymisation
values and the scrub token; the verification-before-commit rule; the bundle's layout, ordering and
digests; `MaxBundleBytes` at 64 MiB; the seven-day single-use hashed grant and the refusal it answers
with; the two permissions, the one role, the state machine and its idempotency keys; the per-module
table above including the reasons `tenant`, `admin` and `web` hold no person column.

The product's share, left to the product: which sector's retention or legal-hold classes outrank an
erasure and how that is worded to the person; every sentence the person reads (the copy tables under
each module's `messages/`, `pt-PT.json` and the rest); the price of an export beyond the free one;
which administrator roles get `subject:erase`, which is a `SeedRoles` line in the composition and not
a fact this module may hold; whether a deployment mails the link at all, since mail is the product's
`Mailer`.

## What will prove it

Each row says what it is: **in** is a case that runs and passes at this revision, **partial** is a
case that runs and covers less than the row claims, and **owed** is debt. The two acceptance lines
of the brief are the first two rows.

| claim | case |
| --- | --- |
| A module with SQL that says nothing about its columns is refused, and the ten reference modules pass the rule they enforce on others | **in**: `TestAModuleWithTablesThatSaysNothingAboutThemIsRefused`, `TestAModuleWithNoTablesOfItsOwnIsNotAsked`, `TestAReasonThatContradictsTheSQLIsRefused` in `kit/module/personal_data_test.go`; and `kit/personaldata`'s own cases read the ten real modules' migration files — audit's `records uuid[]`, auth's eight person columns, `site_settings.logo_file_id` and `users`' none at all — through the same `Check` the boot runs |
| A test exports a seeded person and finds their rows from each module | **partial** — `apps/platformkit`'s seeded case (`TestExportingASeededPersonFindsTheirRowsFromEveryModuleThatHoldsThem`) walks the person it seeds — user, task, content, notification — and not every contributor; the file, auth, audit and change parts are asserted by their own owners' cases, not by this fixture. Beside `erasure_audit_test.go`: seed a person with a session, a notification, a file, a task, a content row, a change proposal and an audit trail; run the export; open the zip and assert each module's part names the row the fixture wrote |
| Erasing leaves no email or name of theirs in any table, while their audit rows remain, anonymised | **partial** — the same fixture, then erase, then one query per declared `Text` column across all eight owners asserting zero matches for the email, the display name and the handle, and one asserting the person's `audit_events` still exist with their `name` and `actor` intact |
| An erasure whose declaration lied rolls back and names the columns | **in** at `kit/module`: `TestAReasonThatContradictsTheSQLIsRefused` refuses a composition whose module declares a text column its SQL does not name, so the boot is the refusal and nothing is written or emitted |
| ~~A module with a person column that declares nothing is refused at boot~~ | in, above |
| ~~Every real module passes the rule it will be refused by~~ | in, above — read from the modules' own embedded SQL, without a database |
| The last reachable administrator cannot be erased | **in** at the owner of the row: `modules/user/internal`'s `TestErasingTheLastAdministratorIsRefused` and `TestTheLastAdministratorCannotBeRemovedThroughAnyDoor`, beside `administration_test.go` |
| A second erasure answers the first receipt and scrubs nothing twice | **in**: `TestTheSecondErasureRequestFilesOneRowAndAsksTheOwnersOnce` and `TestTheSecondErasureOfTheSamePersonAnswersTheFirstReceipt`, beside `TestARedeliveryCertifiesOneRemovalOnce` |
| The seven-day link is single-use, hashed at rest, and answers the same refusal expired or forged | **in**: `modules/subject` cases (`TestTheLinkOpensOnceAndTheRowHoldsNoToken`, `TestAnExpiredLinkAnswersTheRefusalAForgedOneGets`) reading `subject_grants` for the plaintext token and finding nothing |
| A bundle that dropped bytes says so | **owed**: a fixture over `MaxBundleBytes` asserting the manifest names each file it left out |
| The export walk is bounded by RLS and not by a `WHERE` | **in**: the walk reaches each tenant through `jobs.PerTenant` and names none, and `TestOneTenantsRequestIsNotAnotherTenantsRow` has the other tenant read no request, mint no link and move no row |
