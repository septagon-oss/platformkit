# Changelog

## Unreleased

**A rolled-back transaction runs the work that waited for the rollback.** `db.AfterRollback` registers an
effect on the tenant transaction its context is inside, and only an ending the database confirmed runs it:
never a COMMIT that returned an error, whose outcome is unknown, and never a system transaction. A
registration whose transaction has already had its ending is refused by both halves of the seam, and work
an ending dropped is named in the log — at debug for the drop a confirmed ending always makes, as a
warning for the ending whose outcome the database left in doubt.

**A deployment names its account-request budget.** `auth.account_requests.quota`
and `auth.account_requests.window` set how many account requests — a sign-up, a
verification resend, a forgotten-password link — one address may make in a window.
Omitted keeps the module's 10 per 15m; an explicit zero or negative is refused at
start-up, as is a quota over `limit.MaxQuota` (2147483646) or a window over the day
`kit/limit` keeps a counter for. A `kit/limit` counter now stops at its integer
maximum instead of overflowing into an error its callers fail open on. The browser
fixture names a development-sized quota so the sign-up journeys pass together.

**A task queue row says when the response is overdue.** `modules/task` declares
`slaBreached` a summary field, so every app composing it shows *Response overdue* on
the queue row, "Yes" or "No" exactly as the record does, read from the stored fact.
The served catalogue's `summaryFields` for `task/task` gains `slaBreached`. Below 640px a
generated list that declares summary fields stacks them under the row's name, and their
sorts under its header (`components.TableColumn.Stacked`), so a phone reads and sorts
them without scrolling sideways.

**Composed pictures have a private image door.** `modules/picture` validates inert
SVG templates and complete values, authorizes each read in the request tenant
transaction, and serves derived `entity.Picture` references through generated
lists. The resource catalog advances from version 2 to 3 for the new scalar and
optional `pictures.composedSource`; native shells must add authenticated SVG
decoding before displaying it. Stored-file UUID images retain their meaning.

**An unanswered COMMIT is resolved, not refused.** A keyed command marks its row in
`platformkit_command_effect` (`migrations/000072`) inside its own transaction. A begun
claim whose lease ran out is now read against it: a mark on that attempt's row answers 409
`IDEMPOTENCY_ANSWER_NOT_RECORDED` as before, no mark runs the command again, and a
COMMIT still executing answers 409 `IDEMPOTENCY_IN_FLIGHT`. An attempt whose COMMIT was
refused or went unacknowledged records no answer, so its retry is resolved by the mark
rather than replaying a 500. An attempt whose key was taken over while it ran, or whose
mark could not be written, is rolled back and answered 503 `IDEMPOTENCY_UNAVAILABLE`.

**A `widget:richtext` field is an editor now, and still a textarea.** The field
draws its own chrome in `ui/forms` — toolbar, selection bubble, `/` command menu,
link popover, status, counter, draft bar — and `ui/assets/js/richtext.js`, a module
only the pages that draw one field load, mounts the vendored Tiptap bundle on it and
keeps the submitted value in step. Images upload to the file API by drag, paste or
file pick; a record that keeps a write count autosaves against its own ETag and
compares on a conflict, and one that does not keeps the draft on the device.
`make richtext-bundle` rebuilds the bundle; `check-richtext-bundle` keeps it under
150 KiB gzipped and equal to a fresh build. The browser cases cover typing, one
surface, a draft kept through a refused save, images by pick and by paste, the alt
prompt and the parsed round trips; storybook, gallery and mobile editing are owed.

**A scoped credential may retire its own key.** `POST /api/v1/auth/tokens/revoke-self`
(`auth-token-revoke-self`, the new `httpx.BearerKey()` declaration) ends exactly the key that authenticated the
request and answers 204; a session is refused with 403, and a dead or absent key with 401 and a Bearer challenge
— the kernel's one 401. The resource catalogue lists it on an `auth/token` entry with `"operations": []`: an
resource declared `httpx.Resource.CommandsOnly` offers none of the five verbs and appears only to a caller who may
call one of its commands. `tenancy.Principal` gains `TokenID`,
set for a bearer caller and zero otherwise, and `modules/auth/contracts.Tokens` gains `RevokeOwnToken` and
`ErrCallerWithoutKey` — an implementer of `Tokens` outside this repository must add the method.
`authtest.RunTokens` is the conformance suite for `Tokens`. See
[ADR 0020](docs/adr/0020-a-credential-may-retire-itself.md).

**The conformance suite hands every case a store of its own.** `cachetest.Store` said only that the suite
"closes what it is given", and the in-process double keyed its store by `t.Name()` — so a case run a
second time in one process was handed the store its first run had closed and every case of the suite
answered `cache: this store is closed`. That is what `-count` costs a suite with a shared fixture, and the
nightly reaches it: `make flakes` runs the push tier with `-count=5` in one `gotestsum` invocation, and
`kit/cache` is in that tier. The case, not its name, is now the identity — `Store`'s contract says that two
calls made with one `*testing.T` are two handles onto one store, which is what "two replicas" means here,
and that a call made with another `t` returns a store that case owns alone; the double keys its map by the
case and forgets the entry in that case's own cleanup. A new case, `every case is handed a store of its
own`, holds the lifecycle claim and proves it inside itself: it closes its own store and mints the cases it
asks about with nested `t.Run`s, so the verdict does not depend on which case runs next — `-shuffle=on`
moves the top level of a package and never the cases inside a suite. The close case is last and says the
order is a decision: a backend with one connection cannot hand every case a store of its own, so the
handle it returns has to release only that handle.

**The delivery ledger moves onto its app's durable.** `kit/appname.Durable` puts the app in front of a
consumer name, and `platformkit_handled` and `platformkit_dead_letters` key their rows by that name, so an
installation that sets `nats.app` after running without one finds every claim it already earned on the far
side of the rename: the same event, handled once already, answered again as a first delivery. `events.MoveLedger`
is the drain for those two tables. It locks the durables it is about to rename and every tenant that could still be the app's by the end of the move, its own and the ones no app is placed under —
an INSERT of a claim at an unscoped durable takes its own tenant's shared advisory lock and only then that
durable's in the schema itself (`migrations/000058_ledger_claim_durable_lock`), and the move asks for the
exclusive ones in the same order and refuses rather than queueing; the tenant's key is what catches the first
claim of a subscription, which is the one no committed ledger row names a durable from, and taking the two in
that order is what lets it catch anything at all, because a claim that reached for its durable first can be
queued behind another move's durable lock having named no tenant, and under that claim a placement commits and
this app's own move answers zero over a handler that is still running — and it moves the claims of the tenants this
app holds: a claim belongs to the tenant it was made in,
and a tenant belongs to one app (`tenants.app`), so the claims of a tenant whose `tenants.app` is empty stay
where the deployment that runs app-less beside this one looks for them. One transaction: a copy of each unscoped
ledger onto the app's own prefix that carries `handled_at`, `name`, `error` and `failed_at` verbatim because the
purge ages on them, then a delete of exactly the rows the copy read, by the keys that copy read rather than by a predicate the `tenants` table could answer differently a statement later, and a last statement that asks whether anything it had to rename is still unscoped and refuses if it is, and one `platformkit.ledger_moved` record
per tenant whose claims moved. A delivery mid-claim holds its durable and its tenant, and the move refuses,
naming itself, having written and emitted nothing. A delivery of a subscription that names no app asks its
tenant's key in Go, above the read rather than beside the row, and then re-reads `tenants.app` inside the
transaction that writes its claim, because the app of its tenant is the one fact about that delivery the placement
changes underneath it, and a second nonlocking snapshot is overtaken by the same placement the first one was while
the INSERT that would have taken the key waits behind a relation lock: a tenant that took an app while the
delivery was being read is not this consumer's to run, and the delivery writes no claim, acks, and leaves the
event to the app the row now names. The table lock this step first took, and a predicate that named the durable
and no tenant, were the defects it replaced: two apps share these tables, so one app's ordinary traffic refused
the other app's move on every boot and every job tick, a refusal that wrote nothing left the window open while
both apps' consumers ran, and walking every unscoped row on the way renamed another app's tenants' claims into a
durable its consumer never listens to — the harm the move exists to close. `kit/app` runs the step at boot,
before any subscription is made, and opens no consumer until it answers (`App.moveLedger`): a refusal is waited
out and logged, not logged past, because a scoped consumer started over a ledger that has not yet moved replays
an event whose claim is already committed under the old durable — a second claim, a second handler run, and the
window the move exists to close opened for as long as the retry takes. The process is up while it waits, its
probes answer and no handler runs; `ledger-move` runs the step again on the scheduler's own lock.
That job, the record's declaration and the channel that describes it belong only to a deployment that names an
app: with no slug there is no prefix to move onto, so the step answers with the zero report and the manifest
declares no event no run could publish. `kit/db`'s `phase=data` window refuses the same act on a table keyed by
something other than the tenant, which is why this is a drain its owner owns in a job and not a migration.
**Naming an app places the tenants that already exist.** An app-less boot stamped `tenants.app` with the empty
slug — 000043's own default for a session that named no app — and the file that proved the NULLs is applied,
checksummed and immutable, so an operator who sets `nats.app` and `app.hosts` afterwards holds a database of
tenants nobody placed and a release that runs nothing for them. `migrations/000059_tenant_app_refusal` and
`migrations/000060_tenant_app_place` are that placement, forward: the explicit mapping wins, a tenant joins the
app whose declared hosts cover every host it holds, and a tenant neither speaks for stops a boot that named
itself, by name and having written nothing, so that operator's corrected retry is the same walk. A boot that
named no app is not stopped, and that half is the line `make check`'s rehearsal drew (v1.1.0 to this tree,
measured 2026-10-05): such a boot asserts nobody's tenant, the copy it migrates is a previous release's database
holding one tenant, and refusing it refuses this release to every single-app deployment, so it places what an
operator's mapping names and leaves the rest exactly as it found them — still on the legacy durable, still
served, because the move renames nothing for a tenant no app claims. The refusal is a function rather than
the `DO` block 000043 carries because a windowed statement cannot read what it has just written — a
data-modifying CTE sees the snapshot it started with — so the placement computes the tenants it could not place
and hands the list over. Two of these walks run at once on a server that holds two apps over one database,
each from its own boot and its own tick, so the write repeats the predicate its reading took from a snapshot:
`platformkit_place_tenants` sets `app` only of a row still empty at the write itself, the competing walk's
UPDATE waits on that row's lock, re-qualifies against the version the first placement committed, and writes
nothing — which is where 000043's "a tenant does not move between apps" is made true, since which relay,
consumer and control-plane scope can reach a tenant follows from that one column. A placement that loses the
row this way is not refused and does not report the tenant as unplaced: it has an app, and the late
declaration is the one that is wrong about the row. That write now declares the tenant's
ledger key too (000061), the key an unscoped claim takes and a ledger move asks for
exclusively, so a placement either stands in front of a move as its contention refusal or
lands behind it and owes the move its own boot runs next. Nor is a tenant that joins behind a move which already
read its ledger a second handling waiting to happen: the mark sits under the durable this tenant's ledger held before the placement, and `events.claim` asks a delivery's own history under both spellings of one subscription's name, so the rename that follows is tidiness and audit and not what the refusal waits on. The declaration has to be on the session that drains, not the one that migrated, so
`db.BackfillDeclaring` carries `db.Declaration` to the drain (`app.Drain` and the worker's `schema-backfill` job
both go through it). `app.tenant_apps_file` is the same mapping as a file of `slug=app` lines for an operator
whose mapping is too long to read in a YAML blob: `kit/config` reads it at load, the inline map wins a
disagreement, and a line that is not a pair, a tenant mapped twice to two apps or an app that is not a slug
refuses the load — a slug naming no tenant yet does not, because the mapping may describe a tenant about to be
created. `pkit.App.Slug` is the kernel half of the composition's own name: `appname.Parse` over the string
`NewApp` was given, asked by `compose`, so a composition named `Collect EU` fails `Validate` and `Build` rather
than shipping an app-less durable under an app-scoped address.
**The envelope carries the app that published it.** `transport.Event` gains `App`, produced and read as the
CloudEvents extension attribute `app`, and a process that names itself publishes at the address with that token
in front (`transport.AppSubject`), while a process that names nothing publishes where this kernel has always
published (`transport.Subject`). A consumer filters its own app's address and, for the length of a rollout, the
addresses the previous build published at (`transport.AppFilters`, `transport.Filters`), because a NATS `*`
matches exactly one token and there is no filter that means "this app, or none". The filter is wide and the
boundary decides: `transport.AddressMismatch` compares the address the broker routed by, the `app` the document
carries and the slug of the process reading, and a message that disagrees is terminated at the consumer with a
log line naming the subject, the event and the tenant — before any handler runs and before any ledger row is
claimed, because a message stored on one tenant's address and stamped as another's would otherwise open its
transaction inside the second tenant's rows. An app that names itself passes only its own scoped address; a
deployment that names nothing passes the two app-less addresses it always passed and refuses any address
carrying somebody else's token. That costs a real deployment something, and the cost is named in
`kit/appname`: a process must not start naming an app while a process still publishing at the app-less addresses
is alive, because from that moment its own older traffic is unreadable to it. A scoped address is not a scoped
ledger, which is what the move above is for; the two are one decision read at two moments.


**A file says what it is, and the library can ask it.** `files` gains `alt`
(`varchar(1000)`, default `''`) and `decorative` (`boolean`, default `false`, under
`file_files_decorative`, which refuses a picture that carries words while claiming it
carries none) in `modules/file/migrations/000063_file_alt_text.up.sql`, and one command
writes them: `Service.SetAlt` re-checks the claim inside the caller's transaction under
`crud.GetForUpdate`, refuses a row that has moved past the `updatedAt` the caller was
shown with `ErrConflict` and no write, and publishes the new `file.altered` in the same
transaction. An empty alt with no decorative claim is legal to arrive at and illegal to
leave by — the upload is never refused for want of words nobody typed. Three reads join
the app surface, all `file:read`: `GET /api/v1/file/files` gained the grid's axes (`q`,
`type` of `all|image|document|video`, `unused`), `GET /api/v1/file/quota` answers the
tenant's own disk from the same sum an upload is refused against and without the upload
lock, and `PUT /api/v1/file/files/{id}/alt` is `file:manage`. `DELETE
/api/v1/file/files/{id}` gains a refusal of its own: a file a live `file_uses` row still
shows answers `ErrInUse` (409) after locking that ledger, and writes nothing — the
removals that outrank a page (a subject's erasure, the two sweeps) keep their own paths.
`contracts.MediaFamily`, `LibrarySort`, `Quota` and `AltClaim` are pure and in
`contracts/`, so the SQL service and `filetest.Fake` answer the grid and the meter
identically; the suite's `Delete` cases now show a file surviving until its last use
ends. `apps/platformkit/testdata/openapi.json` is re-recorded for the three operations.

**The media library has a page at the address its nav entry names.**
`/app/file/files` is served by `modules/file/internal` (`internal.MountLibrary`) in the
shell the composition hands it, so the nav entry this module has declared since the
module existed no longer leads to a 404, and `apps/platformkit`'s
`knownUnservedNav` debt loses a line by being served. The grid is `Search` over `q`,
`type`, `unused`, `sort` and `page` — filters as query parameters, so a filtered view is
a URL — one tile per row and never one per width, the meter is `Quota` (no bar on an
installation with no ceiling), and `?file={id}` opens one file's panel server-side: its
facts, the preview at the stored frame, "Used in" read from the use ledger, the caption
form, and the removal. Two page posts join them, `POST /app/file/files/{id}/alt` and
`POST /app/file/files/{id}/delete`, both `file:manage`, both real forms because a `<form>`
cannot send the JSON verb `kit/httpx` guards a cookie-bearing unsafe request against; the
JSON doors are untouched. The caption carries the `updatedAt` the panel was drawn from,
so the second copy of the same form is refused 409 and writes nothing, and the removal is
disabled while a use row lives while the command underneath refuses it regardless.
The controls a caller may not press are drawn absent rather than disabled, on the
composition's own answer (`Deps.Library`, a `*file.LibraryChrome` with the shell and the
authorizer; `apps/platformkit`'s `media` module supplies it), and every sentence the page
speaks is a key in `modules/file/messages/pt-PT.json`.

**The wire gate reads the plan feature a door declares.** An operation's authorization
declaration has three parts — its `kind`, its `permission`, and the plan feature a route
names with `httpx.Auth.Needing`, which `kit/httpx` marshals into `x-platformkit-auth` and
asks the tenant's plan about at request time (a refusal answers `402` with
`PLAN_EXCLUDES`). The compatibility identity in `kit/wire` was a list of key names that left
the feature out, so on a document whose only other member had moved, dropping a feature,
adding one to a door that named none, or renaming one were accepted with zero breaks — and
`UPDATE_GOLDEN=1` then rewrote the reviewed artefact over a plan gate the runtime still
enforces. The identity is now the whole emitted declaration, spelled `kind`, `permission`,
`feature`, then any other member by name, with a name or a value that does not read as one
member quoted so one member cannot be read as another pair, so all three are B6 refusals
naming the operation
and both declarations, and the composition's one reviewed pair (`kind=signed_in` to
`kind=any_credential`) covers no feature it does not spell on both sides. A pair written
against a declaration with no feature keeps matching exactly what it matched, which is why
117 of the reference document's 119 declarations are untouched and its golden is not
regenerated. Enforcement is unchanged, so this is a gate that stopped missing a change, not
a door that started refusing a request.

**Any declared field of a record can be said in the tenant's other languages.**
`modules/translation` holds the saying, not the text: one `translations` table
whose identity is `(tenant_id, module, entity, record_id, field, locale)` —
`translations_identity` in
`modules/translation/migrations/000043_translation.up.sql`, with `ENABLE` and
`FORCE ROW LEVEL SECURITY` around the policy that matches
`platformkit_tenant_match` — where a row is one field of one record in one
language and is never a record, a lifecycle or a list of its own. A field declares
itself once, `i18n:"translatable"` beside its own `json` and `ui` tags, and no
second list of translatable names exists anywhere: the read overlay, the generated
form, the write body's description and the record screen's per-language badges are
all read from that declaration. The module declares no permission and mounts no
route (`Permissions: nil`, `Routes: nil`), because the record is the door —
`kit/rest` mounts `GET …?lang=`, `POST …/translate`, `…/review-translation`,
`…/suggest-translation` and `…/untranslate` beside the record's own `PATCH`, behind
that `Spec`'s own read and write grants — so a caller translates a record exactly
as far as they may write it, and a key meaning "may translate" could only open a
row its holder cannot read. A read answers the locale's rows with every field that
fell back named in `_i18n` and why (`missing`, `outdated`, `machine`, `withheld`),
and a machine draft no person has reviewed never reaches a public reader. A
translation remembers the source it was made from: `source_text` and `source_hash`
are written inside the source row's own `FOR UPDATE` and are not re-based when the
source moves again, because the stale pair is what a side-by-side review exists to
show. Each write carries the expected revision of each field it names, checked
over every field before any is written, so a save that loses on one of six writes
none of them, publishes nothing and returns no stale row; `translation.updated` is
the module's only event, committed beside the rows it describes, and there is no
`audit.Record` call in this module because the trail subscribes to that event.
Neither a machine translator nor its price is composed here — the provider's
address, key and price are an installation's facts (`translation.Deps.Translator`),
and where none is configured `Suggest` refuses and writes nothing — and which
records a translator may work on is the product's line, written in
`apps/platformkit/product.go`.

**A client that names no accent gets the palette's, resolved on read.**
`modules/site`'s `DefaultPrimaryColor` is the empty colour: not a colour but the
absence of one, stored rather than filled in, so a row says "nobody chose" instead
of recording whatever the installation's accent was the day it was written.
`contracts.Palette` asks the composition for that accent per mode — answered from
`pkit.App.Theme(pair)` over `Wiring.Skin()`, the seam `modules/web` already reads for
the sheet it composes — and both of the service's reads resolve through it, so no
screen, shell or device is handed the empty colour where its own document says
`#rrggbb`. A save of either accent clears it back to the absence before validation and
before the diff, so the settings form that submits what the read gave it writes
nothing, publishes nothing and moves no revision; the event and the trail keep
speaking stored values. `migrations/000054_site_default_accent.up.sql` moves the
column's default and runs no `UPDATE`: a migration cannot tell a colour somebody
typed from the one that column used to fill in, so every existing row keeps it.

**A change to somebody else's row is proposed, decided and applied through four
commands.** `modules/change` holds a proposal over another account's row: the
 digest of the bytes a verdict was made about, the state machine that makes a
`decision one-way` (`proposed → approved → applied`, `proposed → declined`,
`proposed|approved → withdrawn`, one outbox event per transition), the actor rule
that an author can neither decide nor apply their own change, and an apply that
writes once at the revision it was reviewed at or not at all.
`change-proposal-propose`, `-review`, `-apply` and `-withdraw` are its only writes,
under `change:read`, `change:propose` and `change:decide`, and
`modules/change/migrations/000038_change.up.sql` (owner `change`) keeps the rows
under forced row-level security with a partial unique index over tenant, subject
and diff digest — which is what makes a second submit of the same diff the same
proposal rather than a race. Which writes need it is not the module's fact: it
reads one switch a composition hands it and declares no `rest.Spec`, catalog
resource or job. The reference application puts two subjects behind that switch, a
task and a site's settings row, with a review queue and a proposal page whose
refusals speak `modules/change/messages/pt-PT.json`.

**A row's write count belongs to the server.** `kit/entity` names it and `kit/rest`
refuses a body that writes it, a conditional write whose quoted revision is stale
(`If-Match` on the API, the form's own revision condition on the generated screen)
and an edit that arrived after the row moved; `modules/task/migrations/000050_task_revision.up.sql`
gives the task one. Nothing the server owns is drawn as a control, so no form asks a
person to type a number that would move the row to it.

**The reference resources read well, in the person's language.** The five
resources the reference application shows — tasks, content, people, plans and
site settings — now declare how they read: the words a person is shown
(`task`/`tarefa`, `Assignee`, `Due`, `Response due`, `Response overdue`,
`Billing period`, `Username`, `Includes`), which field names a row and which two
say what it costs to wait, which integration columns are kept off every screen
(`source`, `sourceRef`) and which are answered on the record and not the row
(`assigneeId`, `author`, `roles`, `currency`, `code`), the tone of every status
and priority, and the short verb on a command's button (`Assign`, `Resolve`,
`Archive`, `Deactivate`) with the consequence a person reads before an archive or
a deactivation runs. `check-sla` is declared `system`, so no screen offers a
sweep's work while its JSON route keeps its guard unchanged. Every declared word
is served in the request's language: `GET /api/v1/app/resources` negotiates
`Accept-Language` through the tenant's own declared set, answers `Vary:
Accept-Language`, and falls back to the English the code author wrote — which is
why no layer ships an `en.json`. The key is derived from the declaration's own
address (`hints.<module>/<entity>.<aspect>`), so no author keeps a second list of
keys, and the resolver is one file (`ui/resource/hints.go`) read by the catalogue
document and the generated screens alike. Two readings become honest: a declared
`enumLabels` map is now drawn (`display.EnumWord`), and a field declared `hidden`
is no longer offered by a generated form. The line a declared `help:` writes under
a control arrives in the request's language beside its label — the two words are
read by one projection, so a Portuguese form no longer asks in Portuguese and
explains in English. `apps/platformkit/reference_reading_test.go` holds the golden
entry in `en` and `pt-PT`, the rule that no hint string is ever served empty, and
the rule that both happen on a drawn screen and not only in the document;
`apps/platformkit/portuguese_completeness_test.go` holds the rule that every key
the resolver derives has its Portuguese copy, which the loader's own parity check
cannot see because each of these modules ships one translated file. The same
change hides the assignee column from a task list, which lets a title fit one
line — and the link that is a row's way in was exactly as tall as one line of
text, so hiding a column took its hit target from 60px to 20px. It is a target of
its own now: `components.RowLink` sets `display: inline-block` and a 1.5rem
floor, so the 24px the browser audit enforces comes from the rule and not from
whether the person's title happened to wrap.

**The kernel publishes its journey steps.** `e2e/steps/kernel.ts` and `e2e/steps/public.ts` ship
in this module, so the version a consumer pins that resolves `kit/rest` and `ui/page` carries the
steps of the kernel's own acts — the sign-in door, the way out, the workspace, the generated
screen and the public frame — beside the code they drive. A caller that kept its own copy of that
walk re-exports these instead; the addresses in `endpoints` are the reference application's
(`apps/platformkit/fault.go` pins them), and a composition reaches them by its own specifier.
`scripts/e2e_step_library_test.sh` refuses a published name that vanished, an import a module zip
cannot carry, an address this application does not serve, and a step whose evidence path, host or
inbox the file guessed instead of asking the run that started it.

**The catalogue says how a resource reads.** A `Spec`, a `Singleton` and a
`rest.CommandOptions` now carry an optional declaration of how a resource is
presented — the words a person is shown, which field names a row, which block a
field belongs to, which columns a header may order by, what a command's button
says and what a person reads before it runs — and `GET /api/v1/app/resources`
publishes it as a `presentation` object on the entry, on each field and on each
command. The four vocabularies it is declared in are frozen in `kit/entity`
(`Icons`, `Tones`, `Formats`, `Visibilities`) and bound to what draws them by
tests inside `ui/`, because a kernel below the presentation layer cannot import
the renderer it must check against. Anything the gate can name refuses at boot —
a field, section, icon, tone, reference or sortable field that does not exist, a
money format on a column that holds no minor units, a command that is both the
primary action and the destructive one — with a message naming the offender and
the list that refused (`kit/rest/hints.go`). The change is additive on the wire:
a key is printed only for a hint somebody declared, so an entry nobody hinted
serialises byte for byte as it did before, `catalogVersion` stays 2, and the
committed catalogue golden is unchanged by this release. Adopting a hint on a
real resource, and translating one, are not part of it.

**A limiter whose counter is queued is not a limiter whose store is down.**
`kit/limit` gave one two-second wall to a connection, `BEGIN`, the wait behind the
row's own lock and `COMMIT`, and answered any of them with an error — which every
caller of a limiter is told to fail open on. A same-key burst is one queue behind
one row, so on a loaded runner the tail of it spent its budget waiting and was
admitted: 75 anonymous submissions at an allowance of 60, none refused. The
counter's transaction now names its own lock budget (`lock_timeout`, one second,
transaction-local), so the server stops that wait and says it did, and an attempt
that spent its budget is answered as a refusal — `ok false`, `Retry-After` the
whole window, and **no error**, because a refusal that travels as an error is a
refusal every composer throws away. `Count` and `Forget`, which have no `ok` to
refuse with, answer `ErrBusy` rather than a number nobody read. A store that
answers it cannot serve still fails open exactly as ADR 0010 said; a store that
answers nothing at all is this attempt's own wall expiring, which is a refusal.
`kit/limit/README.md` now states the four outcomes and the two budgets for every
composer of them.

**A tenant's people sign in with SAML 2.0.** `modules/auth` mounts three app-surface
routes — `auth-saml-start`, `auth-saml-callback` and `auth-saml-metadata` — for a
composition that can resolve a tenant's identity provider, and `modules/tenant` carries
that provider as six columns an operator sets with
`POST /api/v1/ops/tenant/tenants/{id}/saml` and clears with `…/saml/clear`, publishing
`tenant.saml_set` and `tenant.saml_cleared`. The service provider is built per request
from the row the `Host` resolved, so two tenants on one installation verify against two
IdPs at two assertion consumer URLs. The callback requires an assertion signed in its own
right, addressed to this tenant's entity ID (an assertion that names no audience names
nobody), bound in the bearer method, current, and answering a request the same browser
started; it then spends the assertion id in the transaction that opens the session, so a
sign-in that is refused leaves the assertion presentable and one that succeeds spends it
exactly once. An address may present sixty assertions a minute and no more: the leg is mounted on
the app surface, past the kernel's Public-only write limit, so it carries its own bound,
refused ahead of the document rather than after it. **Breaking:** `auth/contracts.Service.Open` takes the sign-in method
(`ViaPassword`, `ViaOIDC`, `ViaSAML`) — an out-of-tree implementer of that contract must
add the argument, and the API-diff baseline records it as `Reset`'s did. Two dependencies
are raised because this path runs on them: `github.com/russellhaering/goxmldsig` to
v1.6.1, the XML-signature verifier an assertion is checked with, whose previous version
carries a known signature bypass (GO-2026-4753), and `golang.org/x/net` to v0.60.0 with
the 2026-10-08 Go security release (GO-2026-6617 and four beside it).

**A kernel module names itself, and the app names it.** Every module of the kernel
now carries a provider value beside its constructor — `audit.Module`, `user.Module`,
`admin.Module` — declaring the contracts it needs and provides, the contributions it
takes, the phase it runs in and the configuration section it reads, and building
itself from the wiring. An application's composition is one list:
`pkit.NewApp("acme").Use(user.Module, audit.Module, admin.Module)`. The constructors
keep their behaviour under the one name free in every package, `New`, which is a
**breaking rename** for any caller that wrote `user.Module(deps)`: the field list is
unchanged, only the name it is called by. Two configuration sections are read by the
kernel now: `deployment.mail.method` (`smtp` or `mailbox`) and
`deployment.payments.method` (`manual`), each refused before any module builds when
the deployment leaves it ambiguous.

**A shared name carries the app.** `kit/appname` is now the one place a name two apps could share is formed:
the event subject and filter, the durable consumer, the job's advisory lock, the session cookie, a rate-limit
key, a stored file's physical path, the CloudEvents `source` and the broker connection name. A server hosts many
apps over one database and one broker (decision 0074 §6), and inside one app the tenant is the boundary — between
two apps a tenant id is only a label, because both sides name their modules with the same vocabulary. The name is
a validated slug rather than a string, and a census test scans the tree for any shared name spelled inline, so the
door cannot be bypassed by a caller who found it slower than writing the string. Every site the census named is now
behind a constructor: the transport, the relay's address, the scheduler's lock, the cookie, the limiter, the local
file store and the AsyncAPI document each take the app rather than forming a name. The slug arrives as one
configuration key, `nats.app`, and an unset one is the single-app deployment, which keeps every name it already
has. The delivery boundary reads the app back twice. `transport.AddressMismatch` takes the app, and an app that
names itself now answers only at its own scoped address — a message routed at the previous build's address names
no app, so it cannot be shown to be this app's, and is terminated rather than opened — and `events.Consume` then
reads `tenants.app` for the tenant the document names and refuses a delivery whose tenant another app holds,
before the handler's transaction opens: an address says only what its publisher claimed, and past that
transaction row-level security is the other app's. A refusal runs no handler and writes no claim, so the event
stays replayable for the app that does hold the tenant. The composition names every consumer it starts:
`kit/app` stamps `Subscription.App` with the composition's own app — `Options.App`, or the `nats.app` slug
when it names itself no other way, and the one value `kit/app.New` resolves is written back to the
configuration its transport constructor and its migration read, so whichever key spoke, the subjects, the
placement of this boot's tenants, the payload contract, the relay's claim, the durables and the job lock all
name that app — so two compositions of one module do not bind one JetStream consumer, one queue group and one
handled-ledger key. And a stored file sits under the tenant whose
request wrote it — `modules/file`'s local store writes `<app>/<tenant>/<key>` and refuses a write whose call
names no tenant, because the key is a UUID and the path is the only thing that says whose bytes they are. The
rollout filter stays wide on purpose and the check is what decides. An unset slug keeps both older addresses, as
it always did.
**A tenant belongs to one app, and the control plane answers inside that app.** `tenants` gained a
non-null `app` column (`migrations/000043_tenant_app`), stamped when the composition creates a tenant and
never rewritten: lookup by host, the active-tenant list, `Get`, `List` and the operator routes over them
all filter on it, the installation a verb audits into included, and `tenants_operator` is unique per app rather than per database. The back-fill proves
its input or refuses. A tenant already in the table joins this app when every host it holds is one the
boot declares in `app.hosts`, or when an operator named it in `app.tenant_apps`; anything else is listed
by slug and the migration writes nothing. `db.MigrateDeclaring` is the one door a boot has for a fact the
database does not hold, checked before a connection opens. A deployment that says nothing about itself
may still migrate a database with nothing to place — which is every fresh installation — and may not
migrate one with something to place and nothing to say about it.
**A durable is shaped so that its own rename is expressible.** A durable is half of the
primary key of `platformkit_handled` and `platformkit_dead_letters`, so a deployment that
starts naming its app leaves every row it wrote under a key its own subscription will never
ask for again: `DeliverAll` redelivers what the transport holds, the claim misses, and a
handler that already ran runs again — and a targeted replay deletes by the exact durable, so
the dead letters go invisible to the one command that reads them. `Durable` now forms
`<app>+<module>-<event>`: the app joins with `+`, which is in no app slug, module name or
event name, so `acme` + `billing` + `billing.plan.created` and `acme-billing` + `billing` +
`plan.created` are two consumers rather than one — and the join behind the app stays the dash
it always carried, so the scoped name is the unscoped one with a prefix, which is the only
move a ledger row allows (the table holds a durable and nothing else to rebuild it from).
Moving the rows is still owed, and the reason it is owed rather than done is now measured: a
`.up.sql` that writes across tenants writes nothing at the migrate role a real deployment
names, because the policies on `tenants` and both ledgers answer a schema file with an empty
set; the kernel's door for a migration write is a `phase=data` drain, which `tenants` can
have and neither ledger can, because a drain windows over the table's single-column primary
key and both are keyed by `(event_id, durable)`; and `scripts/check_gucs.sh` refuses the
shortcut a reader might reach for — the file raising the runner's own system-access marker.
T-0228; that move, the envelope's `app` field, the installation-scope control plane and the
reference composition's own slug are listed under *Limits* in `kit/appname/README.md`.

**The sequel to a kernel write has an operator's door.** Two commands lived behind a `psql`
connection: the ledger move (`POST /ops/v1/ledger/move`) and, at that row's own replay address
on the same plane, the delivery of one outbox row again by its id. The kernel mounts both
(`kit/app/ledger_ops.go`) at the installation host, where they answer as JSON, and every tenant
host answers them as an unmounted address, before routing. Each is guarded by its own
operator grant (`platformkit:ledger_move`, `platformkit:event_replay`), and only a
composition that names itself mounts them — an app-less deployment has no scoped durable to
move onto, so the routes would be doors onto a zero report. Each body names the app it acts
in and a process answers only for itself; and because a replay names a row rather than a set,
the locked row is asked the same question of its own tenant (`tenants.app`), so one
installation cannot clear the claims of an event another app's consumer will carry. Both
answer with what they did — the move its four counts, so a caller can tell "already moved"
from "found nothing to move", and the replay the event's id, name and the reason it was
given — and every refusal (the wrong app, a delivery mid-claim, an id whose outbox row is
gone) writes nothing, emits nothing and says which of them it was.
`platformkit.ledger_moved` is a kernel event beside
`platformkit.event_replayed`, declared under the same condition, so the audit trail of each
tenant whose ledger moved holds the act and the actor: nobody for a boot, the job's name for
the retry, a person for the verb.

**A lifecycle verb answers at its own door, and both sides of the act keep a row.** Four were missing:
`POST /api/v1/ops/tenant/tenants/{id}/rename`, `…/reactivate`, `DELETE …/hosts/{host}` and
`POST …/delete` — until them a suspended customer had no way back, a wrong name could not be corrected,
a name a tenant answers at could not be taken away, and ending a customer was not something the control
plane offered. Each publishes its own event (`tenant.renamed`, `tenant.reactivated`,
`tenant.host_removed`, `tenant.deleted`), and each of the seven verbs that move a lifecycle now writes
*two* audit rows in the transaction that wrote the column: one in the customer's own trail and one
`tenant.lifecycle_recorded` mirror in the writing app's own installation, so "who suspended Acme, and when" is answered
without reading a tenant's rows. `audit_events.traceparent` carries the request's W3C trace into both
(`modules/audit`'s `000035_audit_context.up.sql` — expand-only, nullable, no backfill),
which is what makes them one act rather than two rows that happen to share a second. A refusal writes
none of it: no column, no event, no stale row read back. Four refusals are the module's own and are
asked before the write — this installation's tenant is not deletable, the primary host and the last host
are not removable, and a request that ends a customer repeats the slug in its body. **No `export`, and
so no `restore`**: the pair has to agree on a format for a customer's rows leaving and coming back, it is
not in this change, and a retired tenant stays retired — `Reactivate` answers it not-found. See
[modules/tenant](modules/tenant/README.md) and [modules/audit](modules/audit/README.md), each under *The
operator boundary*.

**A tenant signs its people in at its own issuer.** The installation had one issuer, one client and
one secret for the whole process (`kit/config.OIDC`, one `*oidc.Provider` behind a mutex): two
companies with two directories were one deployment, and one of them was wrong. The fact moves to the
tenant — six columns on `tenants` (`000030`), resolved per request inside the transaction the request's
own `Host` already resolved — and the mechanism stays in `modules/auth`, which asks its composition for
a provider through `contracts.OIDCProviders` and never learns that `modules/tenant` exists. Two
operator routes name a tenant's provider (`POST /api/v1/ops/tenant/tenants/{id}/oidc`, `…/oidc/clear`),
and the registration mode is the tenant's own column — `disabled`, `existing`, `provision` — not a
process-wide flag. The row holds `oidc_secret_ref`, the *name* of where the secret is and never the
secret, because a row is copied into the outbox and from there into the audit trail. See
[modules/auth](modules/auth/README.md) and [modules/tenant](modules/tenant/README.md).

**A second factor gates every door that opens a session, and a person's sessions and keys are theirs
to read and revoke.** TOTP enrols with the secret shown once and is proved before it is trusted; a
recovery code spends once and rotation replaces the set; the password leg now halts with a 401 that
says the password was right (`ErrFactorRequired`), and the door a tenant's own provider walks a person
through asks the same question of the same account, so a provider that proved only the first half opens
nothing — the refused person finishes at `POST /api/v1/auth/challenge/verify` with their own code, and
the one withdrawal that is refused is a person's last factor.
`GET /api/v1/auth/sessions` lists what a person has and revokes one or all, one `auth.session_revoked`
per row its own statement removed, at `/app/auth/sessions` in the shell. `POST /api/v1/auth/tokens`
mints a bearer key scoped to permissions its holder already has, which dies at its expiry, is not a
credential at another tenant, and cannot widen itself back to its holder — and `httpx.AnyCredential()`
is the fifth authorization declaration for the one operation that names no permission and spends none,
because the catalogue is the first request a client that is not a browser makes and the only credential
it holds is a key. `000031` and `000032` are the two tables; `modules/auth/internal/surface_test.go`
pins the module's whole surface against the record the kernel's own mounts write. **No passkey**: the brief asked for one, it is not in this change, and the module README
says so under *Open here*.

Re-recording follows in the same branch: `apps/platformkit/testdata/openapi.json` gains these sixteen
operations, and rule B6 of its wire gate refuses a change of a door's authorization that could refuse a
caller it used to admit — the one widening it allows, `signed_in` to `any_credential`, is named in the
table beside it.

**A denial names the permission and who can grant it.** The page a signed-in person sees when a guard refuses
them used to say only "Não pode fazer isto." / "You can't do this", although the guard's detail carried the
permission it asked for. It now keeps that verdict and adds "You need the <permission> permission for it. Anyone
who manages roles here can grant it: ask your administrator.", in the request's language. A shell whose catalogue has only the
short sentence keeps its language and gains the permission's name. From three UX walkthroughs on 2026-09-30, in
which a staff member could not tell what to ask for; T-0184 adds asking for it.

**A card with an address is a link.** `components.Card` rendered an anchor only when `Clickable` was set as well
as `Href`. A card with `Href` and `Hoverable` got a hover shadow over an `<article>` that went nowhere, which was
every card on the pets adoption list: a UX walkthrough found an adopter could not open a single animal's page.
`Href` now makes the card a link with the clickable styling, and `Clickable` alone still styles a card that
handles its own interaction.

**An event leaving the outbox is a CloudEvents 1.0 envelope, and the tenant is
in its address.** The wire form was `transport.Event`'s own struct tags —
`{"id","name","tenantId","payload","at","actor"}` — a private shape nothing
outside this repository could read. It is now CloudEvents 1.0 in structured
content mode, with `tenantid` as a required extension attribute and
`traceparent`/`tracestate` as the distributed tracing extension, and the subject
is `platformkit.<tenant>.<module>.<event>`: a tenant's backlog is an address, a
durable can be per tenant, and a bridge routes a customer without opening the
payload. The envelope decodes the pre-envelope shape and never writes it; a
subscription answers both addresses while that window is open, because a NATS
`*` is one token and an unread decoder is not a window. `module.Module.Declared`
names the Go type of each payload; that projection is the JSON Schema the outbox
refuses a mis-shaped payload against — a member the projection cannot describe
constrains nothing, and it no longer panics the door either — and the AsyncAPI
3.0.0 document `apps/platformkit/testdata/asyncapi.json` is rendered from it, as
each message's `payload` where a validator reads, and checked in by
`make check`. A manifest outside this repository keeps the list it already wrote:
`Events` still takes event names, and `Declared` is the field that takes
`events.Declare[contracts.Changed](contracts.EventChanged)` beside them. An event named
without a type is published unchecked and counted as uncovered — the state a nil payload
already meant — and a module that wants its payload in the document, and refused at the
outbox before the row is written, declares it. `events.Replay` is the operator's verb for a terminal delivery, and
`kit/trace` carries the W3C context from a request into the outbox row — it fixes the
format and collects nothing; the spans, the provider and the exporter are the entry
below. That carrier now
bounds the caller's `tracestate`: `trace.Parse` keeps whole entries up to
`trace.MaxTraceState` (512 bytes) and drops the rest, because the string is stored in
the outbox row and republished on every event the request caused, so an unbounded one
is a header paid for per event; the trace itself survives either way, and a state too
large to keep is dropped whole rather than cut mid-entry. See
[the rollout notes](kit/events/README.md): the subject change recreates every stored consumer, and the claims are what make that safe.

A replay now requires its actor as it requires its reason, and the record it
refuses to write is the one that would have named nobody. `Purge` leaves an
outbox row a dead letter still describes: the row is the payload's only copy, and
a terminal failure the operator can read but never run again, with nothing left
saying what it carried, is the evidence this change set exists to stop losing.

**A test that must name a tenancy setting to read it can be exempted from the GUC gate, in a reviewed row.**
`scripts/check_gucs.sh` reads text, so a test that matches a migration's `set_config('platformkit.…'` line as
a string looks like a write. `scripts/gucs-exempt.txt` lists such files, one `<path> <reason>` per line.
Only a `_test.go` file may be listed, since a test never ships, and a row without a reason is refused. Every
exemption is printed on every run. Assembling the name from pieces to slip past the gate remains the wrong
answer, because the gate then reads nothing.

**A module declares the addresses it moved, and the kernel redirects them.** `module.Module.Moved` is a list
of `{From, To}` whole paths. The kernel answers an old address, or anything under it, with the same redirect
as its own migration table: 302 for a safe method, 307 for a write, the remainder and the query kept, never
cached, no body. The surface gate refuses a route mounted where a row redirects. `module.Validate` refuses a
relative path, a row pointing at itself, and one old address claimed by two modules. A module that moves a
page now writes one row, not a handler for the old address (T-0104 had written 988 lines of them across
eight modules).

**A module's own workspace pages stand instead of the generated register.** The admin shell used to mount
generated screens for every registered resource, even at addresses a module already served with its own
page. The surface gate refuses two routes at one method and path, so such an application did not start.
The pets client could not boot for exactly that reason (T-0126). The shell now leaves a resource's screens
to the module when the module recorded any GET at or under that resource's screen address, and logs it at
boot.

**Every page's stylesheet is four cascade layers, and a consumer sheet gets exactly one
of them.** `ui/css` emits `@layer` again: `ui.Compose` writes `@layer tokens, base,
components, client;` as the sheet's first statement, puts the palette and role variables
in `tokens`, the preflight in `base`, every resolved class list — the components' and
the extras' together — in `components`, and every hand-written sheet a module or an
application hands it in `client`. A layer ranks before specificity, and for normal
declarations a later layer wins, so the client layer is the strongest of the four and
what keeps a consumer's rule off a component is the gate below and not the ranking
above: a consumer can neither write a later file nor name a more precise selector to
win, because the name is refused first. The gate at
that boundary is `ui.Compose`: a consumer sheet that states an `@layer` of its own,
carries a brace, a comment start or `</style`, begins a rule with an at-keyword or a
`;`, names a kernel-rendered attribute or one of the classes the sheet's own
markup carries — by a `.`, or by comparing the contents of the `class` attribute —,
`:root`, a `--pk-` property or a raw colour — each read as a name, in any
spelling a browser resolves it to, and a colour in its hex form or in any
functional notation (`rgb()`/`hsl()`, `lab()`/`lch()`, `oklab()`/`oklch()`,
`hwb()`, `color()`, `color-mix()`) read over the value a browser computes, with the
argument of a `url()` reference and the contents of a quoted string stepped over
because neither computes a colour — is refused by panic — such a sheet is Go source
wired at mount, and a refused composition ships no bytes rather than a stylesheet that
lost a layer. The gate is the contract of the sheet a page links, and `ui.Compose` is
the one place it is enforced: `ui/export` composes the same four layers with
`ui.ComposeDesign`, which places every rule identically and refuses none, because a
design capture measures what a browser computes for a sheet a mount would refuse — an
authored colour with no token yet, a margin on the icon of a component someone is
proposing — and the design-export suite, which is where that distinction showed, is a
CI step rather than part of `make check`. That is a breaking change for a sheet written against an earlier
kernel: composed against this tree, exported consumer sheet builders in the products
checkout refuse that compose against the pin they carry today, each on a raw colour in
a `box-shadow` or a
`background-image`, or on a selector that names a kernel hook or one of the kernel's
utility classes; a sheet assembled inline sits outside that count, and the count
itself belongs to the sweep's own checkout rather than to this one.
The cure is the client's own share — `css.VarRef` for the colour, its own hook or its
own class for the selector (T-0139, T-0126).
**The sidebar panel now gives way to the line drawn across it.** Review round 21 found
the fifth ground a shipped rule paints a component's edge on, and the first one no gate
named. `clSidebarSectionHeader` draws `BorderColor(style.BorderPrimary)` and declares no
fill of its own; `clSidebarColumnAdmin` fills the column it sits inside with
`Bg(style.SurfaceInverse)`, which is the `sidebar-bg` token — a colour a seed decides and
a client may file. Over the corpus that round read, 35 of 800 readings fell under 3:1,
worst 2.65:1, and a `design.yaml` the door accepted measured 2.05:1 there. The pair is
gated at `MinContrastGraphic` in both vocabularies — `bodyContrast` names
`border-default` on `sidebar-bg`, and the new `PanelEdgeRolePairs` names
`border-primary` on `surface-inverse` — so `Client.Resolve` refuses a file that erases
the rule and `FromSeed` refuses a seed that does. It is a fifth list rather than a fifth
ground of `edgeRolePairs` because the two kinds of ground give way in opposite
directions: the page grounds are light in a light theme, so the line is repaired dark
against them, while the panel is dark in a light theme because it carries the theme's own
light copy, so the panel is the half with room — `generatedTheme` now darkens it under
the line (`settleDarker`, the walk toward black alone). Over 400 generated seeds that
leaves no reading under 3:1 (narrowest 3.10:1), refuses no seed, and costs the separation
nothing: the closest pair of the corpus is 0.0006 where it was 0.0005, and colliding
pairs went from 2840 to 2578. 254 of the 400 light sidebars moved, 29 of them to pure
black, which is where the arithmetic puts them: an edge the surfaces force to luminance
.10 can only reach 3.0 on black, so the 3.5 the generator aims at is out of reach and the
walk reports the far pole. `edgeRolePairs` still names the page's own surfaces alone, and
review 16's coverage case — which measures every rule that declares no fill against every
ground in that list — stays green, which is the check that the split is a real one:
`clInputError`, `clInputTone` and `clTabsUnderlineActive` draw a status tone or the brand
colour with no fill, are painted on the page and never inside a sidebar, and no danger red
that reads on a light card can reach 3:1 on a panel dark enough to be a sidebar.

**A tenant's brand colour is now measured before it is painted.** Review round 19 found
the third door a colour comes through. `design.yaml` is refused by `Client.Resolve` and
a generated palette is repaired by `FromSeed`, both of which are doors a file passes;
`site_settings.primary_color` is a `char(7)` a tenant edits, and `modules/web` wrote it
straight into `:root{--pk-color-accent-default:…}` on every page of that tenant's site.
Unlayered, that declaration outranks the `@layer tokens` block `ui.Compose` emits —
which is the point of a per-tenant palette — and so paints `fg-link`, `fg-brand`,
`surface-brand` and the label on it from a value no gate had ever read. Six hex digits
was the whole of the defence: `#c0ffee`, which this package's own test pinned reaching
the head, measured 1.10:1 as a link on a light card, and `#0f5d4e` — the accent the
kernel itself ships in `light`, which a tenant is invited to copy into a dark site —
measures 2.36:1 as the accent and 2.16:1 as a link on the card beside it. `Pair.LegibleAccent` is the measurement: the colour as it stands when
the finished theme clears `Theme.Check` and `Theme.CheckRoles` with it, or the nearest
colour along its own hue when it does not, the same walk `FromSeed` repairs a generated
accent with, run against the pair the caller holds. An illegible answer is a refusal and
the page keeps the accent its own stylesheet carries. The site asks once per mode, because
the one value paints two schemes it cannot both read: a `system` site now declares the
light answer at `:root`, the dark answer at `[data-theme="dark"]` and in the same
preference rule `ui/style/theme.go` uses, and a visitor who switches modes sees a colour
that reads in the one they are in. `#2563eb` is the colour that measurement is asked about: the shipped default, what
the column and every installation that never opened the settings screen paints, reads
4.4981:1 on the light canvas — under the 4.5:1 floor by two thousandths — and 3.2618:1
as `fg-brand` on a dark card, and the paint answers with `#4164b0` and `#618ef1`, which
read. The default was moved to *no colour* partway through this delivery and is
withdrawn: the device contract publishes `primaryColor` with
`pattern ^#[0-9a-fA-F]{6}$`, `apps/platformkit/wire_compatibility_test.go` rule B3
refuses a change to a published shape whichever way it moves, so an empty string is not
a value the field may hold, and the round trip a client takes — read the site's colour,
save it back — was answered 422. A site whose row holds none is still painted the accent
of the palette its own sheet links, mode by mode, which is where the arithmetic no
single `#rrggbb` survives (body copy has to read on grounds 4.5 x 4.5 = 20.25:1 apart,
and `design.Default()` puts them 18.28:1 apart) is answered: by the door that paints,
not by a constant. Letting a tenant *choose* "none of my own" over the API is the
product's share, and it is a B3 break to the device contract — it ships the way
/api/v1/admin did, with a new address and an alias row, not inside a fix round.
`SiteSettings.Validate` fills an empty colour with the default and refuses whatever else
is not six hex digits: legibility is decided where the colour is painted, because a row
saved before this gate existed is painted by whichever code answers the page, not by the
one that wrote it.

**The site's quotation bar is now gated the way the field's edge is.** `bodyContrast`
named the boundary of a text field and no other, while `modules/web/internal/style.go`
paints a second one: `[data-prose] blockquote` carries a 4 px left rule in
`--pk-color-border-strong`, on the page canvas of every article the site serves. That
token is named by neither half of the gate — no pair in `bodyContrast`, and no role in
`design.RoleLayer()` references it, so `CheckRoles` could not see it, and
`Client.Validate`, which accepts any colour a theme exports, stopped at "is it a
colour". A `design.yaml` filing `border-strong: "#f2efe7"` — the light canvas's own
value — was issued a palette whose quotation bars measure 1.00:1 on the page they are
drawn on, while the table's line in the same sheet, drawn in `border-default`, was
refused. `bodyContrast` now gates `border-strong` on `surface-canvas` at
`MinContrastGraphic`, the floor SC 1.4.11 asks of a boundary, and on that one ground
alone: the rule this package has always used is that an unpainted pair has no business
in the list a client is refused by. No client's screen moves — the generator already
repairs the token to the body floor, so across 4000 generated seeds in both themes the
closest reading is 4.995:1, and the shipped pair reads 5.01:1 in `light` and 5.62:1 in
`dark`. What changed is the refusal, and it names the token to the client that erased
it. `design/README.md` carried "every rule above the tokens is written in terms of a
role", true of the utility and class-list layers and false of the three hand-written
sheets; the sentence now names them, and names that every token they paint is one the
token gate reads. One shared loader's fixture (`kit/designconfig/testdata/clients/pets`)
wore a real client's slug and sector and now wears `lantern`/`crafts`.

**A component's edge is now gated on every ground the kernel paints it on, including
the one the layer mixes.** The entry below named two grounds for a line this
repository paints on four. `clAlertVariant["neutral"]` and `clMediaRefused` draw
`BorderColor(style.BorderPrimary)` on `surface-tertiary`, and `clButtonVariant`
`["secondary"]` and `clModalCancel` keep that same line while
`hoverBg(style.SurfaceHover)` moves the control's own fill out from under it — a
ground no token names, since `--pk-role-surface-hover` is `HoverTintPercent` (4 %) of
`text-primary` mixed into `surface-primary`, two tokens a client may both override.
Nothing refused a palette whose hovered button's boundary disappeared: the shipped
dark pair read 3.10:1 there, and a `design.yaml` naming `border-default: #6f6c69`
was accepted by the door at 3.01:1 on the card while painting the same boundary at
2.71:1 on the fill under it (and 2.56:1 on the muted panel). `edgeRolePairs` now
gates `border-primary` on `surface-primary`, `surface-secondary`, `surface-tertiary`
and `surface-hover`, `bodyContrast` names the three literal grounds among them, and
the generator repairs the emitted edge against the hover mix as well — which needed
the ground to be mixed before the edge is chosen, so `TextPrimary` is now set ahead
of `BorderDefault` in both branches. The shipped pair moved with it rather than sit
on the new floor by a hair: `border-default` is `#85827d` → `#7a7773` in `light` and
`#60756f` → `#6b807a` in `dark`, at the margin the generator emits (worst reading
3.51:1 `light`, 3.53:1 `dark`, on the four gated grounds; `surface-active`, still
ungated and still painted by no rule, now reads 3.75:1 and 3.23:1). Across 400
generated seeds (800 themes) the closest gated reading is 3.50:1, no seed is
refused, and the client pinned at the edge of the gate since round 9 (`light
surface-primary: #0df7f7`) is still accepted, at 3.27:1 on its own hovered fill.
`surface-active` stays ungated on the same argument this package has always used: a
pair nothing paints has no business in the list a client is refused by.

**A text field's edge is now measured, and the sentence that exempted it is
corrected.** `design/contrast.go` stated its own scope as "Borders and tints carry
no information and are not listed" while `ui/components/classlists.go` paints one
border as the only visual information identifying a component: `clInput` fills the
field with `style.SurfacePrimary`, the same role `clCardFrame` paints the card with,
so the fill says nothing about where the field is and `clInputNormal`'s
`BorderColor(style.BorderPrimary)` is the line that does. WCAG 2.2 SC 1.4.11 asks
3:1 of exactly that — its worked example is a text input's border — and the focus
ring, which carries less, was already gated on three grounds while this was gated on
none: measured on the shipped pair, the edge read 1.69:1 on the card and 1.50:1 on
the page canvas in `light`, 1.44:1 and 1.56:1 in `dark`, and 1.42:1 at the worst of
40 generated palettes. `bodyContrast` now names `border-default` on
`surface-primary` and `surface-canvas` at `MinContrastGraphic`, and `edgeRolePairs`
spells the same two pairs in the layer a browser paints
(`--pk-role-border-primary` on `--pk-role-surface-primary` and
`--pk-role-surface-secondary`), so `Client.Resolve` refuses a client whose own
override drops its field edge under the floor and `ui/export` refuses it too. The
generator emits the edge at 3.5:1 rather than at the 3:1 that gates it, because the
ground a client files is not the ground the generator drew: a client that names its
own `surface-primary` moves the card the field stands on, and an edge emitted at
exactly the floor is refused for the kernel's colour when the client changed its
surface. Measured after the change: the shipped pair reads 3.43:1 on the card,
3.33:1 on the canvas and 3.02:1 on the muted panel in both themes, and across 400
generated seeds (800 themes) the closest reading is 3.90:1 — the muted panel is
repaired against but not gated, because no shipped rule draws a field's line on one
today, which is a fact about the components and not a floor left unmet (the entry
above gates that panel and the hovered fill, and the numbers below are the ones the
entry above supersedes). Six colours
move and nothing else does: `border-default` and `border-strong` in each shipped
theme (`light` `#cbc5b8` → `#85827d` and `#8f988f` → `#636763`, `dark` `#2c3b37` →
`#60756f` and `#5c6d67` → `#81928c`) and, in a generated pair, the same two tokens
at the same floors; the two role declarations mixed from them move with them. A
native screen is handed `--pk-color-border-default` as a colour, so this reaches
`platformkit-mobile` through the token document, whose pinned digests are
re-recorded here with their measurement — 12 of the pair's 196 measured lines
differ (a theme prints 28 tokens and 70 resolved values: the 22 colour tokens, their
passthroughs and 48 `--pk-role-*` variables), and no class, rule or example HTML
does. Decorative dividers stay exempt: a line between two rows the
reader has already found, and a tint whose copy the body pairs measure themselves.
What is still ungated, and named rather than quietly dropped: `sidebar-bg` under a
ringed control, and `surface-active`.

**A focus ring is now measured on each of the surfaces the kernel draws it on.**
Review round 10 found the graphic's floor read against one ground of three:
`clFocusRing` pairs `Ring(style.Ring2).RingColor(style.RingFocus)` with
`RingOffset(style.RingOffset2)`, which puts the 2 px ring *outside* the control and
onto whatever surface the focused control sits inside — `clCardFrame`'s
`surface-primary`, or the muted surface a panel raises itself onto — while
`bodyContrast` measured `focus` against `surface-canvas` alone. `tokens: {dark:
{focus: "#905546"}}` therefore resolved with no error at exactly 3.000:1 on the
page and 2.692:1 on the card and 2.305:1 on the muted panel, under the 3.0:1
SC 1.4.11 floor the same file states; swept through `Client.Resolve`, 286 311 of
465 088 candidate overrides were accepted and 51 385 of their readings sat under
the floor. `bodyContrast` now names `focus` on `surface-canvas`, `surface-primary`
and `surface-muted`, which is what makes `MinContrastGraphic` mean what its own
comment says: one gate, applied to a generated theme and an override alike. No
shipped or generated colour moves — `design.Default()` reads 4.09/4.62/3.71 in
`light` and 7.38/6.78/5.96 in `dark`, and over 4000 generated seeds (8000 themes)
no ring reading falls under the floor, the worst at 3.395:1 — so the two new lines
refuse nothing the generator emits and nothing this repository ships. The role
layer needs no fourth list for the ring: `ring-focus` is a bare reference to the
`focus` token and the three surface roles bare references to the three surface
tokens, so the two measurements agree to the last digit, which
`design/ring_ground_test.go` pins. The fourth ground a ringed control sits on,
`sidebar-bg` (`clSidebarBrandLink` and the sidebar's own disclosure buttons merge
`clFocusRing`), is measured and held by the shipped palette at 3.57:1 and 7.62:1,
but is not gated: naming it would refuse 1762 of those 8000 generated readings
(worst 2.408:1), so it asks for the ring to be repaired toward the sidebar the way
a tint is repaired toward the copy it carries — a colour decision, named for the
follow-up brief with the number.

**One downstream copy still wears the value this branch rejected.** This branch's
gate moved `light` `text-muted` to `#586461` because `#5f6b65` measured 4.382:1 on
`surface-muted`, under the 4.5:1 text floor. `platformkit-mobile` regenerates its
palette from this repository's design export and still pins the old value
(`testdata/design-tokens.json` → `src/ui/tokens.ts`, provenance-pinned to upstream
commit `5fc585926ce9a7d31af131f26a08ae9e967a591d`), so the phone paints the colour
this branch refused. Nothing in either tree fails: the refresh is a deliberate act
in that repository, provenance-pinned the way `catalog.json` is, and cannot be
priced from here. It is recorded rather than left silent.

**The label a filled tone button paints is now a measured pair too.** Review round
8 found the door, not the palette: `clButtonTone` replaces a button's whole
appearance with the tone it is given — `Bg(style.SurfaceDanger)` under
`TextColor(style.FgOnBrand)` at `text-sm`, which is what `ui/resource` puts on the
delete form of every generated list — and no list the gate reads paired
`fg-on-brand` with a status fill. `bodyRolePairs` names no `fg-on-brand` at all,
and the status list added above gates the *soft* tints, so the pair fell between
them; `accent-on` is one of the 22 tokens a client may override, so
`tokens: {light: {accent-on: "#aacc44"}}` resolved with the success button's label
at 3.497:1 while every pair the gate does measure held. `StatusRolePairs()` now
names `fg-on-brand` on all four status fills, and `BodyRolePairs()` names it on
`surface-brand` — the same label on the accent the token gate already measures as
`accent-on`/`accent-default`, spelled the way a stylesheet paints it — so one
sweep of the painted rules covers a button and a tint together. No colour moved:
`design.Default()` holds every one of these pairs, and 20 000 generated seeds reach
the floor with the worst at 5.958:1, so the certification `FromSeed` now runs over
them never fires on generated output. `Client.Resolve` refuses the override above,
and `tokens: {light: {accent-on: "#66ccff"}}` at 3.56:1, naming both roles. The
component sweep no longer skips a ground whose name does not end in `Soft`, the
narrowing whose own comment this round disproved; the single ground it composes
aside is `surface-overlay`, the sidebar's own tint over the sidebar.

**A gated role pair is now the same pair read from a component or from a
stylesheet.** Review round 7's failing case derived the pair a failed media panel
paints out of `ui/components`' own declarations — `Bg(style.SurfaceWarningSoft)`
under `TextColor(style.FgMuted)` — and asked whether `design.GatedRolePairs()`
names it. It could not: the list spelled every role as the emitted CSS property
(`--pk-role-fg-muted`) while the pair read off a Go constant spells it as the layer
declares it (`fg-muted`), so the gate's own vocabulary decided the answer. The two
spellings are now one rule owned by `design`: the pair lists in `roles.go` are
written in the declared vocabulary, `RoleLayer` still hands out the emitted one, and
`CheckRoles` resolves either against the layer it is handed — refusing a name that
is neither, a token name included, and quoting the emitted property in the refusal
so `Client.Resolve` and `ui/export` still name the same line. `ui/style`'s mirrors
re-spell design's lists for the seam that ships a sheet, and
`TestRolePairMirrorsSpellTheEmittedNames` pins the two to one declaration; no
emitted CSS, token document or digest pin moved. (`ui/components`'s
`TestPaintedSoftPanelPairIsGated` now passes; the pair itself was already gated and
certified by the entry above.)

**A status tint now carries the copy painted on it, not only the tone named after
it.** Review round 7 found the kernel painting a body-size foreground on a ground
no list measured: `clMediaFailed` gives a failed media panel
`--pk-role-surface-warning-soft` and `mediaAbsent` fills that panel with
`clEmptyDesc`, 14 px copy in `--pk-role-fg-muted`. `design.Default()` holds that
pair at 5.47:1, which is why no page in this repository shows it. Of the 400 seeds
round 7's case generates, one theme paints it under 4.5:1 (4.475:1 at
`legal`/`Client211`); over the 1200 seeds round 7 probed, 6 themes fell under, and
the same muted line on a danger- or info-tinted panel — one merge apart — fell
under on 702 and 679 of them, to 3.57:1, while every gate in the repository stayed
quiet. The gated layer gains a third list, `design.StatusRolePairs()`: each status
tone on its own tint and `fg-muted` on all four. The generator repairs a tint
against that muted tone, walking it toward white in a light theme and black in a
dark one, and `design.FromSeed` now refuses its own output over `GatedRolePairs()`
and not only over the 22 tokens — a generator that certified only tokens is what
let the pair through. `Client.Resolve` therefore refuses `tokens: {light:
{status-warningbg: "#d0ccbc"}}`, which holds every token pair and paints the
muted line at 3.75:1. A generated client's warning, danger and info tints move a
few units toward the page, and the DTCG digest pin is re-recorded for it; the
shipped palette, the stylesheet and every token pair are unchanged.

**The gate a client's file is validated by is now the gate a stylesheet is built with.**
Review round 5 swept the override space this branch opened and found 98 of the 23115
overrides `design.Client.Resolve` accepts have a gated role pair below the floor: a dark
theme whose `surface-primary` is the warm neutral `#2e2920` holds every one of the 22
token pairs and measures `--pk-role-fg-brand` on `--pk-role-surface-brand-soft` at 3.91:1
— the label of a brand badge, under the 4.5:1 the badge's own copy requires. The cause was
ownership, not arithmetic: the `--pk-role-*` declarations lived in `ui/style`, which
`design` may not import, so `Client.Resolve` — the door a `design.yaml` actually passes
through — could run only the token half of the gate while `ui/export` ran both. The
declarations move down to `design/roles.go` as what they are, names, references and
percentages, and `ui/style` renders them (`RoleVars`, `RoleColors`, `BodyRolePairs` and
`TintedRolePairs` are the same API as before, delegating) while `design.Client.Resolve`
gates the finished pair with `RoleLayer()` and `GatedRolePairs()`. The rendered layer is
byte-identical: the digest pins over the shipped stylesheet, the token export and a
generated client's DTCG document did not move. `Theme.CheckRoles` now refuses a caller
that hands over no pair to measure, and `RoleLayer` detaches each declaration it hands
out, because a mix is a pointer and one shared table would let a caller editing what it
was handed move the layer every later caller in the process measures.
**The contrast gate learned to measure the layer a browser paints.** Review round 1 measured the
delivery's own claim and found it standing on the wrong layer: `design.Theme.Check` reads the 22
`--pk-color-*` tokens, while a component sets `color: var(--pk-role-fg-secondary)`, and
`ui/style` mutes some roles toward the surface they were authored against. On the shipped palette,
`--pk-role-fg-tertiary` measured 3.49:1 on `--pk-role-surface-tertiary` and 4.35:1 on
`--pk-role-surface-primary` — the same role, two surfaces, and every one of the 40 generated palettes
reviewed failed the same way at 3.01–4.46:1, while passing `Theme.Check`. Three role values are
re-authored: `--pk-role-fg-secondary` and `--pk-role-fg-tertiary` mix `text-primary` with
`text-muted` (two colours the gate certifies on every surface) instead of with `surface-primary`, and
`--pk-role-fg-placeholder` — the label of an empty field, which is read and is not exempt — is
`text-muted` outright rather than `text-muted` walked 30% toward the surface under it, which measured
as low as 1.62:1 on a raised card. Measured as a byte diff of `ui.Compose(design.Default())` against
the previous commit, exactly these three declarations changed and nothing else; the two digest pins
that cover them were re-recorded. `design.Theme.CheckRoles` / `design.Pair.CheckRoles` now measure a
caller's role declarations against the pairs it names, `style.BodyRolePairs()` lists the 18 body-size
text pairs `ui/components` composes (a disabled control is absent: SC 1.4.3 exempts inactive
components), `bodyContrast` gained `text-primary` on `surface-muted` — the pair a neutral badge
paints and nothing gated — and `ui/export` refuses a pair whose role layer is under the floor at the
seam where a pair becomes a stylesheet or a document. What stays open, and is now named rather than
assumed: `--pk-role-fg-brand`/`--pk-role-fg-link` are the accent, and the accent on `surface-muted`
measures as low as 3.82:1 in dark on generated palettes — neither gate reads that pair today, and
closing it means either holding the generated accent to a fourth surface or refusing more clients.

**The client register is the only door from a design.yaml to an identity.** Review round 1 found the
collision rule enforced on one of two routes: `designconfig.LoadClientDesigns` compared every pair it
loaded, while a composition that read one directory at a time and called `design.Client.Resolve` per
client met no such rule and could wear two identical palettes. `LoadClientDesign` now returns a
`LoadedClient` that keeps its declaration unreadable, and `designconfig.Register` (`Add`, `Pair`,
`Pairs`) is the only route from it to a `design.Pair`, so the set's rule is not a step a caller can
omit. The same change closes a quieter one: the set loader used to `fs.Stat` a directory and then skip
it when the stat and the read disagreed, so a client whose file the filesystem refused to hand over
was missing from a returned set that reported no error — the quiet substitution of `design.Default()`
for a client the package doc refuses to accept. A directory with no `design.yaml` is still not a
client (`fs.ErrNotExist` is skipped by name); any other read failure fails the whole load.

**A client's identity is generated from a seed, and the gate measures it.** Seven of the eleven
homepage.css sheets the client repository holds (19 client directories, counted here on 2026-09-28)
express their whole visual identity in three to five raw hex values, and no code this repository can
build computed a contrast ratio — the one implementation in the workspace is in pk-design, which no
`go.mod` here requires — so nothing could refuse a bad four. `design.Seed{Sector, Name,
Brand}` now generates both themes deterministically, and refuses its own output below WCAG 2.2's 4.5:1
for the fifteen body-role pairs — the focus ring is held to SC 1.4.11's 3:1, because the standard asks
that of a graphical object and asking more would refuse a ring nobody mistakes for text. Running the
gate over the palette this repository ships found one: light `text-muted` measured 4.38:1 on the muted
surface and moved to 4.85:1. `clients/<slug>/design.yaml` carries a seed plus at most named overrides
of the 22 colour tokens, validated by the same gate over the finished pair; two clients whose palettes
a reader would take for one another are refused at load rather than rendered side by side. What stays
open: those sheets still write hex and `apps/platformkit` still composes `design.Default()`; this
delivery moves no client onto the loader, and the loader and the reference palette are what a client
can be moved onto.

**The gate now refuses what it cannot measure, and a client's shape reaches the document.** Measuring
the delivery found three holes in it: an override carrying alpha is premultiplied, so a translucent
foreground read as a darker opaque one and `transparent` — which parses — measured 21:1; a second
font-stack regexp beside the client validator admitted stacks the package's own `ParseFontFamilies`
refused, so they failed later, in the export; and the radius grammar lived in that regexp rather than
beside `design.Shape`, and admitted `%`, which means a different shape per container and has no DTCG
representation. A colour with alpha is now refused before anything is measured, a font stack is checked
by the one parser the renderer uses, a radius by `Shape.Validate` in `px|rem`, and `design.Shape`
decodes the spelling a person writes (`card-radius`). Shape then rides the rest of the way:
`TokenMode` carries the three `--pk-radius-*` dimensions and `TokenExport.DTCG` emits them, pinned by
digest, so the corners a client names reach `platformkit-mobile` instead of stopping at a Go value.
Two refusals that had no failing case now have one: two clients on one seed are refused as one palette
worn twice, and a third client whose own override breaks its own body role refuses the whole set rather
than booting a roster with a client missing.

**The reference application's personas are declared, and each is proven to do its own
journeys and be refused the others'.** `apps/platformkit` seeds `coordinator`
(`task:read`, `task:update`) and `observer` (`task:read`) with every tenant, beside
auth's `admin` and `member`. It refuses to compose if a persona grants a permission
nothing declares or one that belongs to the operator. `persona_test.go` drives seven
journeys as each of the four roles at the shipped composition and holds a table of
allow/deny. Resolving another person's task is refused to the administrator and the
coordinator by the task policy, not by a grant.

**Object scope is decided by a policy the composition writes, and every refusal is
audited.** `kit/tenancy/providers/opa` embeds Open Policy Agent (Apache-2.0) as a
`tenancy.Policy`: a composition writes its rules in Rego, they are compiled once at
start, and a decision names its revision, the source's content hash. An undefined
decision is a refusal and an answer that is not an object with a boolean `allow` is an
outage, never an allow. The reference application composes the task module with
`apps/platformkit/policy/task.rego` — an assigned task is its assignee's to resolve —
so the object-scope question decision 0011 asks is answered by a running rule and not
only by a hook. `tenancy.WithPolicyRefusals` lets the HTTP layer observe each refusal
`tenancy.RequirePolicy` returns, so a `POLICY_DENIED` from deep in a module's service
reaches `security.denied` with the action, reason and revision, as `AUTH_DENIED` does.

Two of these entries, one property: a tenant keeps somebody who can administer
it. Each was written because the state was reachable, not because a race was
reported, and each says below what it leaves open rather than leaving that to a
reader who depends on it.

**The kernel measures itself: one trace per request, and the tenant on every
number.** Nothing in this runtime emitted a number at all — the register was
`log/slog` JSON, and `go.mod` named OpenTelemetry zero times — so following one
request ended where one process ended. `kit/telemetry` now holds the vocabulary and
nothing else (the attribute keys, the request id as W3C Baggage, three instruments,
a closed set of refusal classes), and `kit/app` installs the one `TracerProvider` and
the one `MeterProvider` against an OTLP/gRPC collector, because that package is the
only one whose closure may hold a provider. A span covers each of the four
boundaries: the HTTP operation, named after the operation id and opened before
routing so a probe and a 404 are inside one too; the transaction under `Tx`, one per
transaction and none per statement; the outbox relay — one span for the pass, and one
beside each event it hands to the transport, on that event's own trace and naming the
tenant of the row it published, because a batch is read across every tenant and no
tenant a pass span could name would name the rest; and the job run, with one span
per tenant inside a run that walks them. The publisher's W3C context — both trace
members and the baggage member that carries the request id — is written on the outbox
row in the transaction that wrote the event, so the request that moved a
state and the handler that reacted to it are one trace and not two that happen to sit
next to each other, an operator reading a worker's span can quote the id the response
header carried, and the trail's own `traceparent` column (migrations/000035)
makes a row of the trail name the request and the trace that caused it. A request
whose caller supplied an id that W3C cannot use as a trace id — a proxy's opaque
handle, which the router accepts and echoes — opens a trace of its own instead of
writing an event that names no trace at all; its trace joins to the log line and the
row through the request id, which all three of them carry. Three numbers, each with a tenant dimension where one is
known: `pkit.http.operation.duration`, `pkit.outbox.lag`, `pkit.http.refusals` by
class — the last counted once per request, from the status the client was finally
answered with, so the refusals the router writes for itself (a query value that is
not an integer, a body that is not JSON, a handler that returned an error) are in it
beside the ones the guards write. A number carries the tenant and never the request
id: a metric attribute is part of a time series' identity, so a value unique to one
request would be one series per request and a dashboard that can average nothing.
The exemplar OpenTelemetry attaches to every measurement names the span it came
from, which is how a number leads to an example of itself. A gauge keeps the value it
was last given, so the
relay that fills `pkit.outbox.lag` also writes the correction: a tenant's series for
an event name it reported a wait for and can no longer find a row of — neither in the
batch it is holding nor in the queue, where another worker may be publishing it this
moment — is recorded at zero by the pass that reaches the end of the queue, which for a
queue that keeps up is the next one, a second later. The read that decides which
series that is asks the queue which series it still holds a row of, and names none of the
ledger as bind parameters: a read whose size follows how many readings this process has taken
is a statement the server can refuse inside the relay's own transaction, and a refused read
there takes the batch with it — the queue's number would then be the reason the queue stopped
draining. It asks once, on that last pass, and not on every batch: what the read costs is the
queue's pending set, a drain takes that set `batch` rows at a time, and a pass holding a full
batch has not reached the state the question is about. Without that correction the
number would answer "how far behind is this tenant's queue now" with the wait of a row
that went out yesterday, for as long as nobody published that name again. The ledger
that owes the correction is per process, which is the scope the instrument has: a
restart starts its collections from nothing. 102 of the 103
boundaries the reference application registers as operations and module declarations —
91 operations, 5 module jobs, 7 subscriptions — carry a tenant on their span; the one
of those that does not is `file-reconcile`, a sweep whose question crosses every tenant
by construction. Both terms are counted over this composition rather than remembered:
an operation is one `operationId` entry of `apps/platformkit/testdata/openapi.json`,
the document the composition serves and which a committed case refuses to let drift
from that file, and the two module terms are the jobs and subscriptions the
composition's manifests register. That tally expressly does not count the five jobs
this package registers for itself — `outbox-relay`, `outbox-purge`, `limit-purge`,
`idempotency-purge` and `schema-backfill` — nor the sixth, `ledger-move`, which only a
deployment that names `nats.app` schedules and whose one pass renames every tenant's
delivery ledger; the run span of each carries no tenant either, for the same
reason `file-reconcile`'s does: each is one pass over every tenant's rows, so the one
tenant it could name would name none of them.
`file-reconcile` is the one module boundary that carries no tenant, not the only
boundary that does not. Each kind the tally counts is pinned
by a case that reads a span back and asks whose tenant it names: an operation in
`kit/httpx`, a transaction and a subscription in `kit/events`, a tenant's share of a
run in `kit/jobs`. The denominator moves when a route or a manifest moves, and the merge that
regenerates that contract owes the count in the same change; the numerator is a construction and a
case per kind, because every operation the count names is served behind the one chain that resolves
the tenant before it routes (the one `a.api.UseMiddleware(a.tenant, a.traced, …)` in `kit/httpx/httpx.go`,
named by what it calls rather than by a line a merge moves) which stamps both tenant keys as it resolves them. A Public operation
answered at a host the loader knows nothing about goes on with neither key — no tenant to name, so
none invented — and the tally counts a boundary by what it can name, not by one request: that
answer is this boundary with no tenant, not a second boundary without one. With no endpoint
configured nothing is installed at all, which is cheaper than a no-op, and an unreachable collector
fails no boot, moves no health verdict and does not decide an exit code: `kit/health` reports its
last success as a reading an operator sees, not as a check, and the last flush of a process that is
leaving is a log line. The reading is kept per export, because the spans and the numbers are two
services on two schedules: one answer for both let a metric tick that arrived clear a span batch
that did not, and an operator asking about traces was answered with the metric pipeline's delivery.
The half that failed is named; the half nobody asked about yet is silent rather than assumed down.
One environment input is refused rather than honoured: OpenTelemetry merges `OTEL_RESOURCE_ATTRIBUTES`
into the resource of every provider it is handed, so a deployment that names `pkit.tenant` or
`pkit.tenant.id` there would export every tenant's spans and numbers under that one tenant's name;
the boot stops, names the key and says what to do instead, and every other attribute the environment
supplies still arrives on the resource.
A bare `host:port` is accepted as the unencrypted endpoint it names, which
is the form the example file prints. A composition notices one change: `health.Register` and
`health.Mux` now take the checks as one slice and the reports as a trailing variadic, so the call
that passed `Check` values passes `[]health.Check{…}`; each report then runs on the readiness
request's own context — the one the checks run on — so a probe that hangs up or times out bounds
the reading instead of leaving it running past the request it answered. Left open on purpose: no
dashboard, which the brief refused, and no collector in a Helm chart, which is a deployment's
stanza rather than this repository's. `make trace` starts a collector for the machine in front of
you — `deploy/otel-collector.yaml`, printing every span and datapoint it receives to the container
log — so the claim that the kernel is traced is checkable without an account at a trace backend.

*Reused:* the existing `X-Request-ID` mechanism, `log/slog`, `kit/problem`'s one
error shape, `health.Check` (a sibling type, not a fork), the T-0018 migration
runner, `otelhttp` as the inbound span, and the SDK's own span recorder and manual
reader as the test doubles. *Added:* `kit/telemetry`, the baggage column beside the
envelope's two trace columns, the trail's own trace column, the four boundary spans,
the publication span beside every event the relay hands over, and the three
instruments.
*Made reusable:* `health.Report`, `telemetry.Tracer` (taken per span, so a span
arrives at the provider the process installed), `telemetry.Propagators`,
`telemetry.SpanAttrs`, `telemetry.MetricAttrs`, `telemetry.Shared` and
`telemetry.WithRequestID`, each
documented at [kit/telemetry] (kit/telemetry/README.md), which carries the same three
sentences for the package itself.

**A migration now says what kind of migration it is, and the runner holds it to
that shape.** A rewrite and a ten-million-row backfill were the same file: one
transaction, no bound, and the expand/contract rule a review comment. A file may
carry a `-- pkit:` header — `phase=expand|contract|data`, with `batch=`, `table=`,
`autocommit=`, and `allow=<rule> reason=…` for the exceptions a reviewer reads
(the marker is read whatever spaces are written inside it, because an unread
declaration is a file applied as a kind it is not) — and the runner then applies it
in the mode it declared: transactionally, as one
nontransactional statement that must be re-runnable, or as a backfill wrapped over a
window of its table's primary key, one committed transaction per window, resumable
from the last key it committed in `schema_migration_backfill` — and from the whole
table when no key is committed yet, which the ledger says as a NULL, because a `text`
key can hold the empty string and the smallest key there is. A drain ends in the
transaction that wrote its last window, not in one after it: "every row written" and
"the version applied" are one commit, and no run can stop between the two and leave a
table that reads as unfinished work. Every file runs with a
five-second `lock_timeout` (configurable, `database.lock_timeout`) and no statement
bound by default — the same budget the worker's batches re-assert, because a backfill
is the fifty transactions that wait behind the running application, not the one file —
and one stopped by the lock budget returns `db.ErrContended`:
nothing new was applied, and it may be run again. A `contract` file refuses while the
`expand=` version it names has not already applied, and nothing of an owner applies
past a drain that has not finished. Which drains a release finishes, and which its
worker's `schema-backfill` job does, is decided by what waits behind the file: a data
file with files behind it is the worker's, because this run cannot reach those files
either way, while the owner's last pending data file is drained by the run itself under
the bound it gives itself — so a boot never waits behind a table it cannot empty, and a
release whose last step is to fill a column does not leave that step to a tick. A body
that says it bounds itself has no window to count, so it stays the worker's even last.
A boot that meets a drain
already in flight resumes it under that same bound and boots whatever the bound leaves:
`db.ErrBackfillBudget` out of a migration is the worker's to finish, not a failed
deploy, while `platformkit migrate` and `Bootstrap` — doors asked to finish — still
report it. The worker's own drain is bounded now too, at ten thousand windows: a tick that
could not stop was a tick that never ended, holding the job's advisory lock while it repeated
work. A refused data file leaves no progress row behind, and the corrected file
then converges through the same door, because that row is what
a resume reads and a file refused for its shape never ran — and the three shapes that
are the window's to refuse are named there rather than answered with a server error: a
body of two statements; a body whose own CTE list binds the window's name `batch`; and a body
that writes the column the cursor is ordered by, which leaves the table never empty of work. The
last two are read as constructs, not as one way of writing them — the CTE name in either spelling
PostgreSQL takes it, with a list inside a sub-expression left to the scope that shadows the window
there, and the assignment target in either shape it takes it, a key merely read by the body left
alone — and what those readings cannot see (an upsert's `DO UPDATE SET`, a body that only appends
rows above the cursor) is ended by the tick's bound rather than by nothing. A body that merely opens with a CTE list of its own is none of them,
and is drained: the window joins the body's list, since PostgreSQL takes one `WITH` per
statement. The rules
the runner refuses before connecting, the keys, and the floors each source declares are
written down once in [migrations/README.md](migrations/README.md); the mode-scoped ban on
nontransactional SQL is the amendment to
[ADR 0011](docs/adr/0011-migration-ownership.md). A rule that documents no exception
cannot be excepted: an `allow=` naming one is refused as the bypass it is. The guards
read operations rather than spellings (`ALTER TABLE t ALTER col TYPE` rewrites the
table whether or not the optional `COLUMN` keyword is there, `ALTER TABLE t DROP col`
takes a name away from the running release whether or not the file spelled the keyword,
and the `DEFAULT` that
makes an added column ordinary is read from that column's own definition and not from
the file), a rule floor is bounded by the source's own highest version rather than by
whatever number a manifest carries, and the guard and the
executor read one normalised text of a data file's body, read where PostgreSQL reads it
(a `--` inside a value is data, the apostrophe inside a `/* … */` is commentary, a
`$tag$ … $tag$` body is one value and an `E'…'` closes past its escapes), so an excepted
body runs once and a body that names its window in another case still gets the window.

**The two things the runner cannot decide now have doors.** `platformkit migrate`
applies the pending schema and exits, over exactly the sources, floors and budgets
every role's boot composes — the retry for a file that came back `db.ErrContended`,
which is an operator's decision to wait rather than the runner queueing inside the
advisory lock every other replica waits on; `--drain` finishes a backfill instead of
waiting for the worker's tick. Every applied file and every finished drain logs the
duration the runner itself measured, and `make rehearse`
is [scripts/rehearse_migrations.sh](scripts/rehearse_migrations.sh): the step a
release runs before it publishes, which lets the previous release's own binary migrate
a fresh database, seeds ten thousand rows per table into a copy of it, applies this
tree's pending files against that copy while sampling `pg_stat_activity` for lock
waits every 100 ms, and exits 0, 1, 2 or 3 — applied, failed, could not run, over
budget or contended. It prints a failed migration's own message the moment it stops,
before any query of its own can fail over a copy the candidate never migrated, and it
refuses to report a measurement it did not take: a watcher that fell short of half the
samples its own watched window resolves to is `LOCK WATCH BROKEN` and exit 2, and every
report names the window it watched and the tree its binary was built from — uncommitted
files included, because `go build` compiles them and `git diff` does not see them. A
copy that could not be dropped is named as
`LEFT BEHIND`. A rehearsal that could not run exits non-zero rather than passing
quietly.

**A seeded grant is only ever as wide as the composition, and there is a door for the
rows an older seeder left.** The operator permissions a new tenant's administrator was
created holding were a list each application wrote out by hand, and no application
narrowed it when it dropped a module: one installation served from 2026-09-22 with an
administrator granted `billing:catalog` and no billing module to exercise it against,
and the only thing that said so was the hourly sweep — fourteen identical warnings by
the next morning. `auth.SeedRoles` now takes the permission catalogue of the modules the
application actually composed (`kit/module.Grants`, which the kernel's own manifest
check already read) and seeds the operator permissions of that catalogue and nothing
else; an initial role naming a permission no composed module defines is refused where it
used to be written, and `contracts.SeededRoles` is what the seeder writes, so an
application can ask what a composition seeds before it has a database. The two rules an
initial role is held to — it may not name the wildcard, and it may not name an operator
permission — are about the permission and not the spelling of it, so `" * "` is refused
as `*` is and `" Tenant:Manage "` as `tenant:manage` is; until now a space or a capital
walked past both and the role was seeded — the row indistinguishable from one that had
been allowed. The rows already written belong to a customer and are not edited on the
way past: `platformkit repair-roles` lists, per tenant and per role, the grants this
installation's own seeder wrote that no composed module defines any more, and `--remove`
is the decision — through the module's ordinary role write, so each removal takes the
tenant's role lock, is held to the floor below and is in the audit as an
`auth.role_set`. What the seeder did not write it does not touch: the built-in member is
seeded holding nothing unless the application's initial roles name it, a customer
tenant's administrator is seeded the wildcard and nothing else, and a permission
somebody added through the roles screen stays where it is and goes on being reported by
the sweep, which is the only thing entitled to say what its author meant by it. The one
row that cannot be said of is the operator's own administrator while it still holds the
wildcard, the only role the seeder writes named permissions into: once a module leaves,
nothing records whether a departed grant was the seeder's or a hand's, so every dead
grant there is taken — each dead either way, and beside a wildcard that still grants the
rest. What `--remove` prints is what its transaction wrote, not what it read. A run reads
a tenant's roles unlocked to find out whether it holds anything to take, and when it does
it takes that tenant's role lock, reads again, and subtracts from what *that* read holds:
derived from the first read instead, the list it writes revokes a permission somebody had
just granted and hands back one they had just taken away, and only the second of those
leaves no trace in what the command prints. Two operators running the command at once
therefore both find the same dead grant, and the second finds the row already cleaned once
it holds the lock and writes nothing at all — so it reports nothing. Two lines for one
removal would tell a tenant twice that a person changed something nobody changed. The
listing a run without `--remove` prints is bounded by that same guard, so a role the write
could not finish is named by neither run and the
sweep goes on being the only thing that says it is there.

**One published signature moved, and that is this change's cost.** `auth.SeedRoles` is
exported from a published package, and its fourth parameter changed type: the
hand-written `[]string` of operator permissions is now `[]tenancy.Grant`, the catalogue
of what the composition declares. Nothing a caller can write repairs the old shape,
because the old shape is the defect; a caller asks its own composition with
`kit/module.Grants`, the call the kernel's own manifest check already made. The second
line is this branch's later one: `auth/contracts.Service` gains `SetRoleChanged`, the
same write as `SetRole` plus whether the row changed — which the write already worked
out for itself, to decide not to publish an event twice, and kept. `SetRole` keeps its
shape, so a caller that renders the role it was given is untouched; an out-of-tree
implementation of the service interface owes the new method, and `authtest.NewFake`
ships one. Measured
with the comparison the release process asks for (`python3 scripts/check_public_api.py
v1.1.0 HEAD`): the v1.1.0 line answers 106 accepted incompatibilities at the merged
tree, and the added lines are these two.
[RELEASE.md](RELEASE.md#choose-the-compatible-release-line) measures a break against
v1.1.0, and the accepted-break baseline is re-measured where a merge joins the two
([scripts/PUBLIC-API.md](scripts/PUBLIC-API.md)); `check-apidiff` is a required line
of `make check`, not a workflow that runs on a Sunday. the `usertest.NewFake` entry below names the same
constraint and the `/v2` migration it owes. The reference composition, this repository's
tests and the repair command are on the new shape; the products repository, whose
`registry.go` passes the literal, is the caller left to move.

**The migration lock belongs to one application's namespace, not to one database.**
`db.Migrate` holds its advisory lock for the namespace it is about to write: the key
carries the OID of `current_schema()` beside its own constant, because the ledger it
protects — `schema_migrations`, and every table an applied file creates — lands wherever
that run's `search_path` resolves. Two applications that share one Postgres database in
separate namespaces therefore apply their own files while one another migrates; two
replicas of one application still reach one namespace through one URL and still queue,
so ADR 0005's boot where one process migrates and the rest wait and find nothing to do
is the same guarantee it was. A run whose path resolves to no namespace at all is refused
rather than locked around, because `pg_advisory_lock(key, NULL)` answers NULL without
taking the lock, and that would be a run that set out believing it was alone.

**The user screen cannot take away a tenant's administration.** Setting the sole
administrator's roles to none, deactivating them and deleting them each answered 2xx,
and each left a tenant where nobody inside it could change a role again: whoever was
left standing was answered 403 by the roles screen and by
`PUT /api/v1/auth/roles/{name}`, because the write they now needed was the one the
tenant could no longer authorise. Those three writes now answer 422 and name the rule.
The rule is `user/contracts.CheckedAdministration`: a tenant keeps at least one
**active** person holding a role that grants `role:manage`. It asks two questions with
two predicates on purpose — `User.Administers` is wide, so removing somebody who has
not accepted their invitation is a write the floor looks at, and `User.CanAdminister`
is narrow, so an unaccepted invitation cannot hold the floor up. The narrow half is
measured rather than theoretical: in this repository's default configuration, which
ships no `mail.host`, inviting an heir as an administrator and standing down answered
200 while the heir's sign-in answered 401. Active rather than "has a password",
because `active` is exactly what `Service.Open` tests before it writes a session, so
the predicate and the thing it predicts agree by construction; counting passwords
instead would refuse writes in a composition that provisioned identity-provider
accounts with no hash, which nothing here creates.

The floor never refuses a grant, so the state stays one grant away from repair; in a
tenant where every holder of an administering role is still unaccepted, every removal
of one of them is refused until somebody active is appointed. `internal.Service.floor`
takes the advisory lock below after the subject's row lock and before its reads, so
two administrators standing down at once cannot both pass. The real service and the
`usertest` fake call the same decision, and the conformance suite holds both to it.

`user.Deps.Administration` is a new **required** field: a composition without it
panics at `user.Module`, as `file.Deps.Storage` does — and so does an adapter
handed to it with nothing inside, because a `&contracts.AdministrationFunc{}` looks
wired and would answer "nobody administers this tenant" to every question the floor
asks. `auth.AdministeringRoles` is what goes inside
`&usercontracts.AdministrationFunc{Ask: …}` — who holds a role is the user module's
table, what a role grants is the
auth module's, and the application joins them rather than either module reading the
other's rows. `Administration` is a one-method interface and the adapter is taken by
pointer, which is what keeps `user.Deps` comparable as it was published at v1.1.0: a
func value reached through an interface satisfies a checker and then panics the first
time two `Deps` are compared.

**The roles screen the navigation always named exists, and it has a floor of its own.**
`/admin/auth/roles` was declared by `modules/auth` and served by nothing, so every
installation logged `admin: a nav entry leads to a path no route serves` at boot and
every operator saw a menu item that answered 404. `modules/admin` now serves the
screen that entry names, guarded by the same `role:manage` the two JSON routes
declare: what each role grants, as ticks over the permissions this tenant may name,
and one button per role that writes the whole list back through `SetRole`. An operator
permission is offered only in the operator's own tenant, because that is what the
write accepts, and a grant the composition no longer offers is named in the form that
would drop it rather than hidden and then destroyed. The list pages at
`ui/resource.PerPage`, as every generated list does. Applications wire
`admin.Deps.Roles`, which stays optional: nil mounts no screen, and a composition
whose navigation names the entry while wiring nothing is told so at boot.

Emptying the last role that grants `role:manage` left nobody able to change a role
again, through the screen or through the route. The rule is
`auth/contracts.CheckedAdministration`, so both doors refuse it and the conformance
suite holds every implementation to it, and `internal.SetRole` takes the advisory lock
before its first read rather than after the read that decides. `ValidRoleName` also
bounds a name at `contracts.MaxRoleName`, the 64 the route and the form already
advertised and nothing underneath them enforced.

**One lock, and one rule behind it.** Both floors take
`"administration/<tenant id>"` through
`pg_advisory_xact_lock(hashtextextended(key, 0))` — the same literal written out in
both modules rather than imported from one, pinned by a test on each side and by a
test across them. That makes the two writes queue behind each other, in both orders.
What the queue was waiting for now exists: the two floors are one rule,
`user/contracts.CheckedAdministration`, called by every path that can move the
property — a role's grants, a person's roles, a deactivation, a deletion — with each
door's own cheap gate about its own row in front of it. It judges two `Reach` values,
the people who could sign in and administer this tenant before the write and the
people it would leave, and it refuses the write that takes the last one away and
never the write that finds none. `auth/contracts.CheckedAdministration` is now that
rule's door on the roles side and asks `contracts.Users.Holders`, so it no longer
counts a role nobody holds as an administrator; it reads those rows read-only, under
the key it already held, and the lock-ordering argument in both files says so.
`TestTheTwoFloorsStillDoNotComposeIntoOneInvariant` asserted the hole and is now
`TestTheTwoFloorsComposeIntoOneInvariant`, which asserts the refusal, beside the
racing pairs (two administrators standing down at once, two administering roles
emptied at once, one of each across the module boundary) and
`TestTheComposedCheckJudgesOneTenant`, which is the case that fails if the people a
tenant is judged by ever stop belonging to that tenant. A refused write still writes
nothing: the row, the domain event and the returned value all go back with the
transaction, and the caller gets a 422 naming the rule and the grant that repairs it.
The attempt is the one thing that survives, because a rolled-back lockout attempt that
leaves no trace is a fact nobody can read afterwards: each door publishes one record
outside the transaction it was refused in — `user.administration_refused` with the
person, the door and the grant it would have taken, `auth.administration_refused` with
the role and the grants either side — written the way `auth.login_failed` already is
(`db.Detached`, the request's own connection, a two-second budget, logged and never
returned), and kept by `modules/audit`, which audits a module by having subscribed to
what it emits.

How bad a lockout is depends on whose tenant it is, and the repair path is narrower
than "there is one". `POST /api/v1/tenant/tenants/{id}/invite` runs in a system
transaction and provisions somebody holding a role the application names — `admin`
here — so an operator can put an administrator into a *customer's* tenant whose people
lost their grants, through a supported route and without SQL. It does not help where
the named role is itself the one that was emptied, since that is the role it hands
out, and it does not help in the operator's own tenant at all: the route declares
`tenant:manage` as an operator permission, held through that tenant's own roles, and
there the control plane shuts and SQL is the only way back. That route is not the only
door of that shape either —
`POST /api/v1/tenant/tenants/{id}/suspend` against the operator's own tenant is a
single request after which every operator host answers as though no site were served,
and `modules/tenant` has no route that reverses it. That predates this work and
neither floor touches it.

The floor sees rows and not people: an active administrator who has forgotten a
password in a tenant with no mail, or whose identity provider is gone, is a lockout
nothing here reports. It is per tenant, so the operator's own tenant gains no extra
protection from it. Nothing here renames or deletes a role, so a held name cannot
vanish today; a module that adds either will need its own floor.

`usertest.NewFake` keeps the signature it published in v1.1.0, and a fake built that
way has no role system, exactly as it did before the floor existed. A test that wants
the floor asks `usertest.NewFakeWithAdministration`, and `TestFakeConforms` runs the
shared suite through it, so the fake and the real service cannot disagree about the
floor quietly. That shape is a release constraint, not a preference: this package is
published, and [RELEASE.md](RELEASE.md#choose-the-compatible-release-line) measures a
break against v1.1.0 with no accepted-break baseline, so changing this signature would
owe the `/v2` migration it describes.

`TestEveryNavEntryLeadsSomewhere` makes the unserved-nav class a gate rather than a
boot warning. Two entries are named as known debt with their reason —
`modules/file`'s `/admin/file/files` and `modules/audit`'s `/admin/audit/events` — and
the gate fails if either becomes served and is left in the list. Serving them needs a
read-only mode in `httpx.Resource` and `ui/screens`, which mount create, edit and
delete unconditionally today.

`make check-race` is called by CI. The goal and the Makefile target have been here
since the concurrency kernel, and CONTRIBUTING has said CI runs it, but no workflow
called it: the gate that would catch a lock released before the commit that needed it
had never run outside a contributor's shell. ci.yml now runs it after `make check`,
and this change widens `RACE_PACKAGES` to `./modules/auth/internal/...` and
`./modules/user/internal/...` beside the defaults, because that is where the advisory
locks above live.

Two direct dependencies moved, and one test helper had to be told why. `dave/dst` goes
to v0.28.0 and `jackc/pgx/v5` to v5.11.0, each on the requirement line Dependabot
proposed; the one graph move beyond them is dst's own `dave/jennifer` 1.5.0 → 1.7.1,
which `go.sum` carries and nothing in `./...` links. Nothing unrelated came with them.
pgx 5.11 rewrote its connection-string
parser to match libpq, which reads `+` as a literal, and `modules/task`'s isolation
DSN was built with `url.Values.Encode`, which writes an encoded space as `+` — so
all 34 `TestConcurrentTaskCommands` subtests refused to connect, asking PostgreSQL
for an isolation level called `repeatable+read`. Percent-encoding the space is
correct on both pgx versions, so that is a repair here rather than something the
bump has to work around. What could reach a consumer is dst's own floor: it moved to
`go 1.26.0`, which this module clears and a pinning module on an older toolchain
would not.

**A command-owned field is refused where a body is read, an empty `PATCH` says
nothing, and a write asks whose row it is.** `refuseImmutable` asked whether the
decoded map held the
declared key while `encoding/json` binds a field a key merely folds onto, so
`{"Author": …}` wrote the author through every generated create route of every
module declaring `Immutable`. The five doors that read a body — create and patch,
the two a page calls beneath HTTP, and `Values` beneath a create form — now ask
it with `strings.EqualFold`, the comparison the decoder itself makes, and refuse
the whole write naming the declared field rather than the spelling it arrived in.
A `PATCH` that named no column moved `updated_at` and published
`<module>.<entity>.updated` for a change nobody made; it now validates nothing,
writes nothing, publishes nothing and returns the row as read. The tenant-scope
recheck a body naming a column triggers still runs first, so an empty `PATCH`
of a row outside the request's tenant is 404 like any other body. The `DELETE`
asked that question of nothing at all: row-level security filters a delete by its
`USING` clause alone, since a delete produces no new row for a `WITH CHECK`
clause to inspect, so on a table every tenant may read, one tenant deleted
another's row — 204, event and all — or answered 500 out of the policy violation
where the Spec soft-deletes. Both doors now ask `crud.RecheckTenant`, the tenant
compare `Update` already made, exported rather than copied: the change's one new
exported name, and a nil entity answers it `ErrInvalid` as every other door in
`kit/crud` does. `db.Now` stamps the three timestamp columns, so an
answer carries the instant the column holds. What stays open: a `Singleton`
declares no command-owned field.
**The address an operation is mounted on now says which surface it belongs to, and a
module no longer writes one.** A module's `Routes` receives `httpx.Surfaces` — the
public face, the workspace and the control plane — and the router composes the address
from the surface and the module: a relative path that repeats either is refused at mount,
so `/api/v1/tasks/tasks` cannot be written again. The workspace keeps its JSON addresses;
what moved is where its documents are (`/admin/<module>/<entity>` is now
`/app/<module>/<entity>`, the shell at `/app/admin/…`), and the anonymous doors of
`auth`, `content`, `file` and `site`, which took `/api/v1/public/…`. Each surface has its
own chain: the public one reads no session, sets no cookie (a handler that mints one gets
`PUBLIC_SETS_A_COOKIE` withheld at the writer at every body size, and a 500 of that
name wherever the response can still be replaced) and is cacheable for sixty seconds;
its anonymous writes are counted by tenant, route and address, so one customer's
office does not exhaust another's budget;
A page whose body its owner can republish asks that cache to revalidate rather than answer
from what it kept, so a publish is on the site the next time the page is loaded;
the workspace is `no-store`, `noindex`, and admits an anonymous caller only at a route
that declares `Public()` — and boot prints those doors rather than keeping a list of them;
the control plane is served at `server.installation_host` and answers as an unmounted
address at every other host — the same status, the same body and the same headers every
other refusal of that host carries, because the host gate runs inside the middleware
that writes them — including to a tenant that is not the installation's, which
is the second half of the tenant-API incident recorded in
[ADR 0015](docs/adr/0015-a-refusal-has-one-value-and-two-shapes.md)'s neighbourhood and
in [`modules/tenant`](modules/tenant/README.md). `modules/tenant`'s routes and
`modules/billing`'s plan catalog therefore moved to `/api/v1/ops/…`.
`GET /api/v1/admin/resources` is now `GET /api/v1/app/resources` and is mounted by
`kit/app`, with the document supplied by the composition
(`app.Options.WorkspaceCatalog` = `screens.Describe`); every catalog entry gained a
`screen` key, a `write_path` key and a command a `path` key — each only where the
derivation from the entry's own `path` is no longer true, so a document that could
always be read the same way still is. The workspace is described as an empty surface
in OpenAPI, stamped `x-platformkit-surface` per operation.

What a caller sees: `/admin`, the admin module's own pages beneath it (`/admin/login`,
`/admin/health`, `/admin/assets`, `/admin/_gallery`), `/api/v1/admin` and the four
public doors answer with a 302 (307 for a write, never a cached 301) for one release,
and are deleted in v1.3.0 — see [aliases.go](kit/httpx/aliases.go).
`/api/v1/tenant/tenants` deliberately has no alias: a control plane does not announce
itself by leaving the old door open at every customer's host. Refusals now name a code
in the JSON `detail` — `AUTH_ANONYMOUS`, `AUTH_DENIED`, `AUTH_NOT_OPERATOR`,
`AUTH_NO_TENANT`, `AUTH_PRINCIPAL_CHANGED`, `CSRF_ORIGIN`, `PUBLIC_SETS_A_COOKIE`,
`LIMIT_EXHAUSTED` (the public surface's own write limit) and
`WRITE_ELSEWHERE` (a write of a resource whose writes are served on another surface,
answered at its read door, naming `write_path`) — where the sentence used to be a
lowercase prefix.

- The kernel answers every refusal it makes for itself in the shape the requester asked
  for: the problem document to a client that wanted a value, the shell's own page — in the
  request's language — to a client that came to be shown one. This includes an address
  nothing is mounted at, an address mounted for other verbs, a CSRF failure and a panic
  that escapes a handler. A guard's refusal — a missing session, a missing grant, a row of
  another tenant, a route the plan excludes, the public surface's write limit — answers the
  same way: it is the same decision, and it is the one a *navigating* person was being
  shown JSON. An htmx write counts as a client that parses a value: its controller
  (`ui/assets/js/htmx-config.js`) reads the refusal's code out of the problem body and
  swaps nothing for a 4xx, so a page answered there would reach nobody.
- Those codes are what a refusal *page* is translated by. `ui/page` holds the one table
from a code to a catalog key (`fault.<CODE>`, and `fault.<status>` for the 404, the 405
and the 500, which carry no code because the kernel wrote the sentence), the page is
negotiated from the request's
`Accept-Language` — a guard answers before a session, a tenant or a stored preference
exists to ask one — and it carries `Content-Language` and `Vary: Accept-Language`. A
translated page keeps the code in front of the sentence (`AUTH_DENIED: Não pode fazer
isto.`), because the code is what a person reads back to support. The
declaration follows the sentence on the page: a shell with no catalog, or none for that
code, says the kernel's English and declares `en`. Two narrower promises came with it:
the pointer to a split resource's write door now goes to a caller holding the
credential that door reads, because the door would refuse anybody else the moment they
reached it; and a tree mounted with `Static` answers a missing file, its own prefix and
any directory beneath it through its surface's chain, in the shape the client asked for,
instead of net/http's plain-text note and a listing of the shell's filenames. The hourly
purge of the rate-limit counters moved from `modules/auth`'s sweep to `kit/app`, beside
the outbox's, because the kernel's public write limit made the kernel the table's second
writer. See
[ADR 0017](docs/adr/0017-three-surfaces-by-path.md) for what this costs a module.

**A module's schema opens to the application role without either of them naming it.**
`apps/platformkit/postgres-init.sql` is where the read role is given `USAGE` on
`public` and its default table and sequence privileges, so a module migration
that created a schema of its own produced a namespace only its owner could open
and every later query answered 42501. The module cannot grant its way out — a
module's SQL may not name a deployment's role — and the migration runner already
refuses to, decoding grantees out of an object's own ACL when it revokes
application access to the ledger. `platformkit_module_schema(owner)`, in the new
`migrations/000026_module_schema.up.sql`, is the one line a module's first schema
revision runs instead: it creates the schema named by that owner and hands each
role the migration role already grants defaults to *in the namespace that schema
opens beside* the privileges that deployment pinned for it there — grantee and
privilege list both read out of `pg_default_acl` rather than written down, one
namespace's rows and no union of several, so a role pinned to `SELECT` stays a
reader inside a module's schema and a role pinned only in some other namespace is
handed nothing by this function — a database-wide pin still reaches a module's
tables, because PostgreSQL applies it there and not because this mirror copied
it. No table moves here — the reference modules
keep theirs in `public` — and the check that every table is scoped to a tenant
stops holding a second copy of its own query:
`dbtest.TenantTablesSQL` is exported, gained the schema column, and matches a
schema against the ledger's owners instead of `current_schema()`, which was the
only schema until this. `migrations/README.md` states the consequences a
deployment can hit.

**A refusal now has an owner that imports nothing.** `kit/fault` declares
`ErrNotFound`, `ErrInvalid` and `ErrConflict`, and `kit/crud` re-exports those same
values rather than declaring its own, so the adapter that classifies a driver error
and the value package that refuses a write before a transaction exists hold one
object and `rest.Fault` answers 404, 422 and 409 once for either name. The package
exists because the alternative was a link: a module's `contracts/`, `events/` or
`domain/` package needing one of the three had to import `kit/crud` — gorm, a
driver, a `db.Tx` in every signature — for an error value. It links the standard library and
nothing else, which its own test asserts against `go list -deps`, the package gate
holds to an empty allowance, and that gate's fixture refuses as out of bounds if it
ever reaches `kit/db` — the edge back into `kit/crud` is an import cycle the
compiler refuses before any gate sees it. The messages do not move and they are not
inert: all three are still what a log line carries, `crud: invalid` and `crud:
conflict` are what a bare refusal reaches a client with as its problem detail, and
`crud: no such row` is not — a 404 is answered with the sentence `kit/rest` holds
for a row this tenant cannot see — while the literal `crud: invalid: ` is what
`kit/rest` trims off a detail to derive the field message a client reads, and
`modules/admin` asserts a person is never shown that prefix: the prefix names the
package that used to own these values, and renaming one is a wire change with
consumers on the other side of it. Eleven of this repository's `contracts/`
packages wrap one of the three under the adapter's name: three of a module's own
contract source — `modules/content/contracts` and `modules/file/contracts`, which
wrap `crud.ErrInvalid` for a body the Markdown renderer refuses and for bytes
that are not the type they were uploaded as, and `modules/user/contracts`, whose
own `ErrRegistrationExists` is a wrap of `crud.ErrConflict` — and the eight
shipped test-support packages one directory deeper, which refuse the way the
service each stands in for refuses. No file of `modules/auth/contracts` imports
that adapter now: this change moved it to `fault.ErrInvalid` and retired its
direct link. `kit/fault/README.md` names all eleven and what each still costs it. No
closure moves for any of them — each names `db.Tx` and imports `kit/crud` or
`kit/db` for `crud.Base` and a transaction-aware service of its own. The
reachability runs from the module to the kit package:
`modules/auth/contracts` reaches `kit/crud` through `modules/user/contracts`,
which imports it for `crud.Base` and not for a sentinel, and reaches `kit/db`
through four of its own files' service signatures; no kit package reaches a
module, which the compiler and `./scripts/check_imports.sh` both refuse. So
nothing here is a gate that now passes. What moves is adoption: the
alias has a value package wrapping the owner rather than only a name, and a
downstream consumer's value package does the same when its pin moves.

**The development stack runs a mail catcher, and the browser journeys read the link they
were sent.** An empty `mail.host` — the default, and what an installation without a relay
has — records every notification and sends none of the ones marked for email. That is safe,
and it leaves a development machine unable to finish the one journey that needs a link: an
invitation nobody can open is the walkthrough the front door was written against. So
[compose.yaml](compose.yaml) composes `mailpit` (SMTP on 1025, its API on 8025, both
overridable the way the stack's other ports are), [scripts/e2e.sh](scripts/e2e.sh) writes the
`mail:` block of the configuration it generates and refuses a run whose catcher is not
answering before it builds anything, and every job that runs `make e2e` (both check
jobs, and the release job) starts the same image, by container name where it can. `e2e/invitation-mail.spec.ts` and `e2e/mailed-links.spec.ts` then search the
catcher by recipient for each of the four account links this application mails — an
invitation, a sign-up confirmation, a resent confirmation and a forgotten-password reset —
and assert what the delivered message says: that its link carries the address and port the
tenant is served at, and that the person it was addressed to can open it and be signed in by
choosing a password there. Two defects came out of writing it. The sign-up's event carried
no served address, so its confirmation link was built from the tenant's name alone and
opened port 80 rather than the port the sign-up was answered on — `user.registration_unverified`
now carries `served`, the way `user.invited` and the two password events already did. And the
forgot page, the confirmation page and the reset page are mounted on two different surfaces,
because that is where auth mounts the three doors: asking the workspace router for a public
door names an alias row that answers 307, which `ui/assets/js/session.js` refuses to follow
by design, so the button told the person the outcome was unknown and sent nothing —
`TestEveryAccountDoorPostsToAnAddressThatAnswers` now reads each page's own `action` and asks
the running application what it says.

**A notice says what it would reach; the module decides whether it does.** The ask used to be
`contracts.Notice.Email`, a bool that either mailed or did not. `contracts.Notice` now carries
`Intent` (what happened, which selects the template and the row of the person's preferences; `""` is
the generic pair), `Class` (an ordinary notice, or `ClassSecurity`, which is told over the person's
opt-out because opt-out exists so people are not marketed to and not so they cannot be told somebody
took over their account), and `Wants`, a ceiling over the five channels `contracts.Channel` names —
in-app, mail, push, browser push, webhook — whose zero value is in-app alone. `Email` is deprecated
and still read: `Notice.Ceiling` folds it into `Wants`, so an application written against the older
contract keeps asking for mail rather than stopping in silence, and a new caller writes
`Wants: notificationcontracts.WantsEmail`. Either spelling gets in-app with it, because the row *is*
the notice and no ceiling a caller raises can fall below telling the person in the application. What
stays breaking is the carrier, not the ask: `notification.SMTP` and `notification.Mail` left with the
SMTP conversation and `Deps` holds a map; the baseline records those, and no line about `Email`, which
came back. `make check`'s `check-apidiff` refuses any break beside the ones that file names.

What the ask buys is a decision, not a send. In the caller's own transaction `Service.Notify` reads the
person's channel choices and quiet window (`notification_preferences`, `notification_quiet_hours`), the
tenant's sending identity (`notification_senders`, one row per tenant, set and verified and removed by
commands that recheck the caller's `sender:manage` grant and the copy's expected revision), and the
channels this deployment wired (`contracts.Providers.Active`), runs `contracts.Decide` once, and opens a
`requested` row and one outbox event for every channel it kept — one event name per channel, so an
endpoint that answers 500 all afternoon delays nothing but webhooks — and a `suppressed` row carrying the
sentence and the `Correction` naming whose decision it was for every channel it refused.
`notification_deliveries` is append-only under the tenant's row-level security and
`delivery_ledger_coverage` is the read that answers "was she told" per channel rather than as a count of
sends; a provider that answers nil closes its row `sent`, `contracts.ErrPermanent` closes it `failed` with
the reason and acknowledges, and anything else writes nothing and retries on the kernel's ladder. Mail
leaves as the tenant that raised it — `From:` signed under DKIM, envelope from `mail.from` — when the
product wires `Deps.Senders` and that tenant's row reads `verified`; a nil port sends `mail.from`
unsigned. [modules/notification/README.md](modules/notification/README.md) accounts for all of it.

## [1.1.1] - 2026-09-18

A tooling patch. No exported API moved and no shipped behaviour moved: the diff from
v1.1.0 is one browser assertion under `tools/designexport/openpencil` and seven lines
of [RELEASE.md](RELEASE.md).

It exists because the Gitea run for v1.1.0 (88) concluded **failure** on the step
*Native browser observations*, and that step drives a file shipped inside this module:
anybody who checked out the tag and ran `npm run test:browser` in
`tools/designexport/openpencil` got the same `TimeoutError`. The suite now follows the
label-to-field association instead of asserting the literal control id
`pk-textarea-description`, which v1.1.0's namespace change had replaced. The full
browser suite is 265 of 265.

The prose change is the part that outlives this defect. `make check` and `make e2e` do
not run `test:browser` — the CI job does, as its own step — and when it failed on
v1.1.0, everything after it was skipped, including `make e2e` and the budget ratchet.
So RELEASE.md now states that an unread CI result is not a green one and that a release
is not ready while the verdict for the exact commit cannot be read. v1.1.0 was tagged
with that verdict unread: that, not the assertion, is the defect being repaired.

## [1.1.0] - 2026-09-18

**This release is not source-compatible with v1.0.0.** The pinned comparison
reports 71 exported changes between them — `kit/crud`'s row types are
`kit/entity`'s, `httpx.Document/Fragment/Script` and `design.CSS` moved into
`ui/page` and `ui`, `events.Memory()` became `memory.New()`, `migrations/` is the
kernel's schema alone, five methods joined `user/contracts.Registrations`, and
`ui.Compose` returns a `Sheet`. No `Deprecated:` shim repairs the 29 that are type
*moves*: apidiff resolves an alias and still reports
`contracts.User.Base: changed from crud.Base to entity.Base`, verified against a
two-revision module before this decision was taken.

It was taken deliberately rather than by omission. No consumer sits on the v1.0.0
stable line — the commercial catalog and the client application both pin
pseudo-versions of `main`, which establish no compatibility — so a v1.1.0 that
keeps the number breaks a build nobody has. It also breaks one nobody has *yet*:
`go get -u` from v1.0.0 now lands here and fails to compile, so **pin an exact
version**. What is owed and not yet paid is unchanged: an outside consumer needs
the `/v2` module path and import migration described in
[RELEASE.md](RELEASE.md#choose-the-compatible-release-line), and v1.1.0 is now the
tag every compatibility question is measured against. The
[accepted-break baseline](scripts/PUBLIC-API.md) that stood in for a release line
went with this tag; the line has no accepted break from itself.

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
