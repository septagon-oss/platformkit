# Seed contract (T-0194; partial implementation)

This is the intended contract for `kit/seed`. Delivered: the loader, reference
orderer, date resolver, state comparator, generic writer service, tenant bridge
and provenance migrations, the reference application's two writers and its
`platformkit seed` command with `make seed`, the starter and demo fixtures under
`apps/platformkit/seed/`, the tenant-create hook that applies the starter to a
tenant being created, and the audit columns that name `seed` and cite the file and
line. Not delivered: a permit type, an `--as` grant check beyond the composition's
own authorizer, and the typed `Problem` codes. Where this document says
`RunCommand`, the kernel door is `app.RunCommand`. Original trace:
`20afc2b0cf43b2bf2a1a046a799e49613243ddbd`.
The existing consumer is the reference application's composition — one file
(`apps/platformkit/modules.go`) when this was traced, and since decision 0074 the
sentence in [`apps/platformkit/app.go`](../apps/platformkit/app.go) with this
product's own values in [`apps/platformkit/product.go`](../apps/platformkit/product.go):
tenant creation calls `seedRoles` and this delivery's seed hook, while the site journey in
[`e2e/site.spec.ts`](../e2e/site.spec.ts) creates and publishes content and saves
site settings through their owners. The focused `kit/rest` Spec boundary test
passed in the worker's trace; the tenant hook test could not reach Postgres.
The source paths above are the durable evidence in this repository.

## Ownership and reuse

Each proposed deliverable has one reuse decision. `kit/seed` imports no module;
the application names every resource writer in a Go slice. The loader, graph,
date resolver, comparator, run service, `seed_keys` and the `kit/db` bridge are
built; the writers, hook, command and fixtures below are not.

| Deliverable | Reuse decision |
| --- | --- |
| Embedded YAML/JSON loader and source locations | **New, because** the trace found no seed loader; use the existing `gopkg.in/yaml.v3` dependency and `fs.FS`. |
| Reference graph, relative-date resolver and dry-run plan | **New, because** the trace found no seed graph, clocked date resolver or plan. |
| In-process generated resource writer | **Composed from** `rest.Spec`'s `createRow`/`updateRow`/`deleteRow` and `kit/rest/screens.go` resource adapter; expose the same guarded write core to an explicit `db.Tx[db.Tenant]`, rather than calling `crud.Create` separately. |
| Command-backed writers | **Composed from** the owning `contracts.Service` (`auth.SetRole`, `content.Publish`, `site.Save`, `user.Invite/SetRoles/SetPassword`, `file.Upload`, `task.Assign`) and the literal application wiring. |
| Demo password generation | **Composed from** the reference bootstrap's existing `generatePassword` in `apps/platformkit/bootstrap.go`; move that generator to a shared Go owner and have bootstrap call it, so no second generator or committed password exists. |
| Tenant creation entry point | **Composed from** `tenant.Hook` and `db.Run`'s typed tenant scope; the same-transaction bridge described below is a **new** `kit/db` operation because no such bridge exists. |
| Persisted demo-tenant marker | **Composed from** `tenant.NewTenant`, `tenant.Tenant` and `tenancy.Tenant`; its new column is read by seed selection and cannot be changed after creation. |
| `seed_keys` persistence | **New, because** the trace found no provenance/key mapping table; its migration belongs to the kernel migration source in `migrations/embed.go`. |
| Audit and trace attribution | **Composed from** `events.Publish`, the outbox trace fields, and audit's `SubscribeAll`; extend their envelope and audit row so source and seed actor survive delivery. |
| Reference command and Make target | **Composed from** `apps/platformkit/main.go`, `apps/platformkit/app.go` and `apps/platformkit/seed.go`, and the existing Makefile command pattern. A downstream flagship owns its own literal client switch. |
| Operator seed transaction runner | **New, because** `app.Bootstrap` can only create the first tenant and the trace found no existing CLI path that authenticates an operator and visits two tenant scopes within one system transaction. |
| Conformance fake and cases | **New, because** the trace found no generic seed `Writer` fake; compose the existing content/task/file contract fakes for their command behavior, and share `seed.Decide` with the future SQL adapter. |
| Reference app fixtures and journey | **Composed from** the existing site and task journeys and the content/site/user/file/task contracts; no client-only module is introduced. |

## Files and grammar

A product embeds `seed/starter` and `seed/demo` with its own `go:embed`, and
passes the resulting `fs.FS` plus root `seed` to `seed.New`. A downstream client
keeps these under `clients/<slug>/seed/<kind>/*.yaml`; the reference app keeps
them under `apps/platformkit/seed/<kind>/*.yaml`. Each file names exactly one
resource; the filename is `<resource>.yaml` or `.json`, where the resource is an
alias from the application's literal writer list. A kind contains at most one
file per resource, even if extensions differ. No filesystem discovery
registers a writer. YAML 1.2 scalar behavior is required; JSON is accepted by
the same loader. Duplicate mapping keys, duplicate record keys, unknown fields,
multiple documents and unsupported schema versions refuse before any write.
The parser preserves `yaml.Node` line and column for every record, scalar and
reference. Source paths are relative to that root, at most 512 UTF-8 bytes;
record keys are nonempty UTF-8 without control characters and at most 1024
bytes. Only regular embedded paths below the passed root are accepted.

The v1 document is:

```yaml
apiVersion: platformkit.seed/v1
resource: contents
prune: false
records:
  - key: home
    fields: {title: Home, kind: page, body: "Welcome."}
    commands: [{name: publish}]
```

`records` is a sequence in source order. Every record has `key`, which is the
resource's existing natural key where it has one. The writer declares its
natural-key field and inserts that key in a create payload; a supplied copy of
that field must normalize to the same key. Duplicate detection uses the
owner-normalized key, so two email spellings or two slugs that normalize alike
refuse before writing. For a keyless resource `key` is an
opaque stable seed name used only in `seed_keys`. Keys are unique within the
resource across starter and demo files; duplicate keys across kinds refuse.
`fields` contains only fields that the owning create/update path accepts. A
declared field its writer's `Target` carries nowhere — an unknown name, or a
read-only field such as a task's `status` or `resolvedAt` — refuses at that
field's own line before the record's row, mapping and event, which is the same
rule `commands` already answers to: a run writes what it read. A declared
*value* the writer cannot hand to its owner answers the same way: YAML reads
`priority: 5`, `kind: 7`, `visibility: 0` and `dueAt: 2026-10-10` as a number,
a date and not as text, and a writer that read one as an empty field would put
its own default in front of an answer somebody did write, commit it as a CREATE,
map the key, and call the row it defaulted unchanged on every run after — so the
value refuses at its own line, naming what it is (`priority is written as the
number 5`). Only a field the record leaves out is an absent one, and a writer's
default answers an absent field and nothing else: `visibility: ""` is declared,
and refuses as the visibility it is, where no `visibility` asks for public. A
field written with no value (`priority:`) carries no answer, so it reads as a
field left out.
`commands` is an ordered list of named owner commands with their arguments;
the writer declares the command names and their desired-state comparator. A
command such as publish is called only while its target state differs. The
seeder never sets a read-only or command-owned field through a generic patch.

Worked examples below are separate files, one per resource. They are examples
of the format, not checked-in data and not evidence that the current modules
already support every path.

**Starter roles (`starter/roles.yaml`, command state):**

```yaml
apiVersion: platformkit.seed/v1
resource: roles
records:
  - key: member
    fields: {grants: [content:read, task:read]}
  - key: admin
    fields: {grants: ["*"]}
```

The auth writer calls `auth.Service.SetRole` with the composition's declared
grants. Operator-only grants are added to the operator tenant's admin role by
the composition, not by a shared seed file. The existing `auth.SeedRoles` direct
INSERT must be retired when this path replaces it; it is not a generic writer.

**Starter pages (`starter/contents.yaml`, natural key and lifecycle):**

```yaml
apiVersion: platformkit.seed/v1
resource: contents
records:
  - key: home
    fields:
      title: Welcome
      kind: page
      body: "Start here."
    commands: [{name: publish}]
  - key: about
    fields: {title: About, kind: page, body: "About this site."}
    commands: [{name: publish}]
```

The content writer injects `slug` from `key` through `content.Slugify`, the
owner's own normalisation, uses the Spec write core for the
row and `content.Service.Publish` for lifecycle. The writer spells the address the
way the module stores and looks it up, so a record keyed `About The Team` meets the
page the module named `about-the-team` instead of writing it again on every run. A separate
`starter/site.yaml` singleton record with `key: settings` and
`fields: {homeSlug: home, title: Example}` calls `site.Service.Save`. Its `key`
is a stable singleton identifier, never a second site row. The site writer
declares `homeSlug` as a reference to `contents/home` and keeps the stored
value a slug; this edge orders the page before the settings write.
The same one-resource file can instead be JSON (`starter/site.json`):

```json
{"apiVersion":"platformkit.seed/v1","resource":"site","records":[{"key":"settings","fields":{"homeSlug":"home","title":"Example"}}]}
```

**Demo users (`demo/users.yaml`, roles and secret):**

```yaml
apiVersion: platformkit.seed/v1
resource: users
records:
  - key: marta@example.test
    fields: {displayName: Marta}
    commands:
      - {name: setRoles, roles: [member]}
  - key: rui@example.test
    fields: {displayName: Rui}
    commands:
      - {name: setRoles, roles: [member]}
  - key: ines@example.test
    fields: {displayName: Inês}
    commands:
      - {name: setRoles, roles: [member]}
```

The user writer injects `email`; creation calls `Invite`, then `SetRoles` and
`SetPassword` through that service. The normal invitation event and its effects
remain in force. The password is never a file field or plan value. On first
creation only, it comes from `PLATFORMKIT_DEMO_PASSWORD`, or from a password this
run generates for that one person. A generated value is handed to the caller that
asked for the run and reaches no output stream on its own: `Apply` collects it,
and `seedCommand` prints it once to stderr *after its transaction has committed*
(`printMintedCredentials` in `apps/platformkit/seed_credentials.go`). Printing it
from inside the write was the old shape, and it was wrong twice — a run that
rolled back afterwards had already published a credential belonging to nobody, and
the same hook also runs inside the server when an operator creates a `demo: true`
tenant through the tenant route, where the process's two output streams are its
log and the operator is on the far side of a socket. `Plan` never generates or
displays a secret, and the server logs none. The generated value is not returned
through a create result: nothing in this checkout carries a secret out of a tenant
hook to a request caller, so **a deployment that creates demo tenants through the
route and names no `PLATFORMKIT_DEMO_PASSWORD` gets people whose credential nobody
can learn** — the hash is written, the password is not recoverable, and the
walkthrough has nobody to sign in as. Set `demo.password`, or create the tenant and
run `platformkit seed --demo`, which prints. What the managed `signIn` fact
compares is whether the row holds a credential at all, so a rerun never resets a
person's password and never repeats the print. Empty or invalid supplied
passwords refuse at the user module's own policy before the row is written.

**Demo binary (`demo/files.yaml`, keyless identity):**

```yaml
apiVersion: platformkit.seed/v1
resource: files
records:
  - key: nina-1.jpg
    asset: assets/nina-1.jpg
    fields: {name: nina-1.jpg, contentType: image/jpeg, visibility: public}
```

`asset` resolves relative to its YAML file within the embedded root, without
`..` or absolute paths, and its bytes are read out of the same `fs.FS` the
records were loaded from: a run that read its records from one tree uploads the
assets in that tree and nothing else. The upload's name is the last part of that
path, a declared `contentType` must be the type that name already has, and
`visibility` is the module's own `public` or `private` — public is what a record
that names none asks for, a third answer refuses the record, and so does a
visibility written as anything but text: `visibility: 0` is a value the record
declared, and reading it as none would store an upload public that its record
did not. A record that wants its bytes read by signed URL only says `private`.
The file writer uploads those bytes through `file.Service.Upload` once, with the
media type the asset's own name carries. It writes
no bytes over an existing upload: a rerun reads back the name and the visibility
the row carries, finds both equal, and uploads nothing, because re-uploading
would leave a new row and new bytes on every deploy with the old ones behind. The
SHA-256 and the media type on that row are on neither side of the comparison, and
the file module is the reason: decision 0069 §4 measures a raster, turns it the way
its camera said, and stores what the frame re-encodes to, with the row's size,
digest and media type read from that pass rather than from what was sent — a PNG
whose pixels carry no alpha comes back a JPEG. A record that compared either would
refuse every image on the run after the one that wrote it, which is a refusal of the
write that is not there to take; the bytes and the container of an existing upload
are the owner's, and no seed command could put others behind the row. It updates
`seed_keys` to the returned ID in the same tenant transaction. No upload is ever
replaced under one key, so `prune` — which files never declare — would concern
removed keys and nothing else. An upload
writes bytes before its row; rollback can leave unreachable bytes for the
existing file sweep, never a committed row pointing at missing bytes.

**Demo page (`demo/contents.yaml`, references, rich text and translation):**

```yaml
apiVersion: platformkit.seed/v1
resource: contents
records:
  - key: gallery
    fields:
      title: Gallery
      kind: page
      body: "![Nina](pk-file:files/nina-1.jpg)"
    commands: [{name: publish}]
    i18n:
      pt:
        title: Galeria
        body: "![Nina](pk-file:files/nina-1.jpg)"
```

Only writer-declared reference fields and rich-text fields are rewritten.
`pk-file:files/nina-1.jpg` becomes `pk-file:<uploaded-uuid>` before the
owner's rich-text write path validates it. Arbitrary plain strings and external
URLs are never scanned
as references. The `i18n` object is reserved for the owner's T-0190 translation
port. Until that port exists, it produces a warning in the plan and makes no
translation write; base fields still apply. Once wired, translations use that
owner's command, with unchanged translations emitting nothing.
This checkout has no `pk-file:` reader in `kit/`, `modules/`, `apps/` or `ui/`
at the resolved revision; the rich-text rewrite requires the named 0069 owner
path before this example can be applied successfully.

**Demo task (`demo/tasks.yaml`, references and relative time):**

```yaml
apiVersion: platformkit.seed/v1
resource: tasks
records:
  - key: take-the-tour
    fields:
      title: Take the tour of the site
      priority: high
      dueAt: "+3d"
      assignee: users/marta@example.test
```

This is the file `apps/platformkit/seed/demo/tasks.yaml`, quoted as it is written.
Task's natural key is `title` — the one line a task is named by, and the writer
declares it — so a run finds a seeded task beside a manually created one with the
same line, and `key` in the file addresses the record within the file. The two do
not have to agree, and the shipped file's do not: provenance maps `take-the-tour`
to the row this run wrote, `title` is a declared field of that record, and the
writer patches it, so editing the line in the file edits the same task. The writer
declares **no commands**: assignment is the `assignee` reference field above, not
a `commands:` entry, and `kit/seed` refuses a command a writer does not offer
before it reads a row. A seeded task is created through the Spec write core and,
where a person is named, assigned by `task.Service.Assign`. A record that stops
naming an assignee neither converges nor half-writes: the module has no command
that takes work back, so the run refuses and commits nothing, because patching the
row's other fields while the assignee stayed in place would report a change the run
did not make, on every run after it.

`dueAt` is resolved against the run's injected clock and applied when the record
is **created** — see *Time, audit and external effects*. The grammar is relative
(`+3d`, `+2y`, `friday 09:00`) and an absolute date refuses the record with those
words: YAML reads an unquoted `2026-10-10` as a date of its own, and a deadline
fixed on disk is one no rerun can honour, so it is refused rather than read as no
deadline. A plain reference has grammar `<writer-alias>/<key>` in a writer-declared
reference argument. The alias
and key are case-sensitive except for the normalization the target's owner
performs: a writer names that normalization in `Resource.CanonicalKey`, and
provenance, the prune keep-set and reference lookup all go through it, so a
reference to `contents/About The Team` and a record keyed `about-the-team` are one
row's two spellings. The target must be in the files for this run, or already
resolvable by its writer in this tenant; a missing target refuses with both
source and target names. Cross-tenant lookup is impossible through the passed
`db.Tx[db.Tenant]` and target writer.

**Prune (`demo/tasks.yaml`, removal):** `prune: true` at the file top means
delete previously seed-owned keys for that tenant/resource/kind which are now
absent from every file this run loaded, through the owner's delete command. A key
the other kind's file still declares is not absent, and a record that moved
between the two keeps its row: its mapping moves to the file that declares it now,
which is what makes the file that let it go stop owning it. No file or `prune:
false` leaves removed records untouched. Pruning uses `seed_keys` provenance for natural
and keyless resources, refuses if the owner protects the record (including
the last administrator), and removes the mapping only with a successful
deletion. A seed file cannot prune a manually created record it has never
owned. An unowned natural-key row that already equals the target is reported
`UNCHANGED` and remains unowned; if it differs, seeding refuses an occupied
key rather than overwriting somebody's manual record. `UNCHANGED` hides no write
to an owner's row; the one thing it can still move is the record's own mapping,
when the file that declares it changed kind.
`Resource` declares whether the owner has a delete command. `prune: true`
for a writer without one refuses at preflight; auth roles and site settings
currently have no such command. A new owner command must exist before those
resources can support prune, rather than a seeder deleting their rows itself.
For example, the following keeps only `welcome-tour` among seed-owned demo
tasks; it leaves unrelated human tasks alone:

```yaml
apiVersion: platformkit.seed/v1
resource: tasks
prune: true
records:
  - key: welcome-tour
    fields: {title: Take the tour}
```

## Public Go API and writer port

The surface is below, marked. `Source`, `Record`, `Command`, `Reference`,
`Decide`, `Clock`, `Deps`, `New`, `Writer`, `Authorizer`, `Key`, `Resource`,
`Selection`, `Item`, `Plan`, `Service.Plan` and `Service.Apply` exist, with the
shapes `kit/seed` compiles: `Apply` answers with a `Plan` rather than a `Result`
and takes no `Permit`, and `Resource` carries `Prunable` and `Commands` but no
`OperatorWrite` or `RichTextFields`, nor does `Deps` hold `DemoPassword`.
`GeneratePassword`, `ApplyCreated`, `RunCommand`, `Result`, `Permit` and
`ReferenceField` are design targets and no Go code names them.
`New` copies its input slice, rejects nil writers and duplicate
aliases or `(module,entity)` pairs, and never discovers modules. `Clock.Now`
is read once per invocation and
converted to UTC. All methods take or receive an explicit tenant transaction.

```go
type Clock interface { Now() time.Time }
type Deps struct {
    Files fs.FS
    Root string
    Clock Clock
    DemoPassword string // application-loaded secret; empty means generate on first create
    Writers []Writer
    Authorize Authorizer
}
func New(Deps) (*Service, error)
func GeneratePassword() (string, error) // moved from reference bootstrap; crypto/rand
func (s *Service) Plan(context.Context, db.Tx[db.Tenant], Selection, Permit) (Plan, error)
func (s *Service) Apply(context.Context, db.Tx[db.Tenant], Selection, Permit) (Result, error)
func (s *Service) ApplyCreated(context.Context, db.Tx[db.System], tenancy.Tenant) (Result, error)
func RunCommand(context.Context, *db.Conn, *Service, CommandAuthority, CommandInput) (Result, error)

type Selection struct { Demo bool } // starter always; demo only when requested and tenant.Demo
type Source struct { File string; Line, Column int }
type Resource struct {
    Alias, Module, Entity, NaturalKey, WriteGrant string
    OperatorWrite, Prunable bool
    References []ReferenceField
    RichTextFields, Commands []string
    CanonicalKey func(value string) string // the identity the owner stores; nil = as written
}
type ReferenceField struct {
    JSONPointer, TargetAlias string
    Output ReferenceOutput // ID or natural key, as the owner field expects
}
type Key struct { Value string; RecordID uuid.UUID /* zero when absent */ }
type Record struct {
    Key string
    Fields map[string]any
    Commands []Command
    I18n map[string]map[string]any
    Asset string
    Source Source
}
type Writer interface {
    Resource() Resource // alias, module, entity, natural-key field, declared refs/commands
    Target(context.Context, Record, Resolved, time.Time /* run instant */) (Target, error)
    Read(context.Context, db.Tx[db.Tenant], Key, bool /* forUpdate */) (Snapshot, error)
    Create(context.Context, db.Tx[db.Tenant], Target) (Snapshot, error)
    Update(context.Context, db.Tx[db.Tenant], Snapshot, Target) (Snapshot, error)
    Delete(context.Context, db.Tx[db.Tenant], Snapshot) error
}
type Authorizer interface {
    Check(context.Context, db.Tx[db.Tenant], Permit, Resource, Action) error
}
type CommandAuthority interface {
    Operator(context.Context, db.Tx[db.System]) (tenancy.Tenant, error)
    Target(context.Context, db.Tx[db.System], string /* host */) (tenancy.Tenant, error)
    Authenticate(context.Context, db.Tx[db.Tenant], Credentials) (tenancy.Principal, error)
    MaySeed(context.Context, db.Tx[db.Tenant], tenancy.Principal) error
}
type Credentials struct { Email, Password string }
type CommandInput struct { Host string; Credentials Credentials; Selection Selection; DryRun bool }
type Permit struct { /* unexported operator/provisioning proof, tenant ID and writer set */ }
type Result struct { Plan Plan; GeneratedPassword string `json:"-"` /* never log */ }
func WithPermit(context.Context, Permit) context.Context
func PermitFrom(context.Context) (Permit, bool)
func (Permit) Allows(db.Tx[db.Tenant], string /* declared grant */) bool
func FromSpec[T crud.Entity](rest.Spec[T], Resource) Writer
func Decide(Snapshot, Target) Decision // one pure comparator shared by fake and SQL writer
```

`Snapshot` holds presence, ID and the canonical values of the seed-managed fields
and commands — nothing a writer may fill that no decision reads, so no revision
field: no owner in this checkout takes an expected revision, and a field that
carries one would be a promise the port does not keep. `Target` holds the same canonical
projection, with resolved IDs and UTC times, but no server-owned fields. The
`Resolved` input distinguishes an existing ID from a symbolic pending
`resource/key`. Dry-run can therefore plan an empty tenant without inventing
IDs; owner validation requiring a real ID is deferred to `Apply`, which
resolves dependencies after their writes and re-runs `Target`. The
writer owns normalization and semantic comparison inputs; `Decide` compares
only the target's managed projection and returns `create`, `update`, or
`unchanged` plus sorted changed field/command names. It never compares raw YAML
bytes, display ordering of a set, a password hash or server timestamps. Every
writer calls this same function: the composition's SQL adapters in
`apps/platformkit/seed.go`, and the map-backed `fakeWriter` of
`kit/seed/service_test.go`. A generated
Spec writer must reuse the Spec's decode, immutable-field check, validation,
event and hook core; its adapter adds a transaction argument and permission
check, not a second CRUD implementation. A command writer delegates every
mutation to the owner's `contracts.Service` and never writes its table.

`Authorizer.Check` runs again inside `Apply` after the tenant and row are
resolved, before each create/update/delete. The reference app supplies an
adapter that checks a human target-tenant actor's writer grant through its
composed auth service, or a provision/operator permit's bounded writer set
and operator restriction. The operator's `tenant:manage` grant was locked and
checked in its own tenant scope earlier in the same system transaction; it is
never inferred from a role row in the target tenant. A tenant-creation hook
supplies a narrowly scoped
provisioning authority for the just-created tenant, as an explicit typed
`Permit` rather than a fake user UUID; it can use only the writers in the
literal composition. The owner services' command authorization ports must
recognize and recheck this provisioning authority, including `user.SetRoles`;
the current `roleGranter.May` requires a human principal and would refuse it.
`Apply` passes the sealed permit in context only while calling an owner command
whose existing port has no authority argument. In the reference composition,
`roleGranter.May` asks `PermitFrom(ctx)` and `Permit.Allows(tx,
auth.PermissionRoleManage)` before its ordinary principal path. That check
compares the permit's tenant with `db.TenantOf(tx)` and its declared grant set;
an absent or wrong-tenant permit falls through to the existing human check.
A CLI invocation must establish an authenticated operator principal before
opening the target's write scope, then recheck that operator's `tenant:manage`
grant and the seed authority in its authoritative transaction. `RunCommand`
owns this sequence and its system token; it exposes only the plan, not a
system transaction handle to application code. `MaySeed` locks the operator's
user and authorizing role rows until commit before it answers; a concurrent
grant revocation therefore cannot race the target writes. The runner then
passes an in-process `Permit` bound to that operator, target tenant ID and
literal writer set. `Authorizer.Check` verifies that binding on every target
write; the permit is not a seed-file field or a bearer accepted from HTTP.
The zero `Permit`, a permit for another tenant and a writer outside its set
all refuse before any mutation. Public `Plan`/`Apply` accept a permit so a
composition can reuse them only through a trusted issuance path; the two
entry points above are the issuers in this delivery.
`ApplyCreated` mints only a provisioning permit from the active system handle,
uses the typed bridge, and selects demo from the newly persisted tenant value.
It cannot turn
an arbitrary `--tenant` or `--client` string into authority. The reference
command's credential input and verification are specified under Limits below;
`kit/seed` never reads an environment variable or host as a grant.

## Plan and refusal shape

`Plan` is read-only and is not an apply token. It validates the entire embedded
set, builds the graph, checks the tenant's persisted demo flag and authorization,
then inspects current rows under RLS. `Apply` independently reloads and
re-decides inside the transaction; an earlier dry run grants no right to write.
The CLI prints sorted, stable text, without field values or secrets:

```text
tenant=example host=example.local kind=starter,demo at=2026-10-01T10:30:00Z
CREATE roles/member                         starter/roles.yaml:4
CREATE contents/home                        starter/contents.yaml:4
CREATE users/marta@example.test             demo/users.yaml:4
UPDATE contents/gallery title               demo/contents.yaml:4
UNCHANGED tasks/welcome-tour                demo/tasks.yaml:4
WARNING contents/gallery i18n skipped: translation writer unavailable
summary: 3 created, 1 updated, 1 unchanged, 0 pruned
```

The structured `Plan` has `TenantID`, `Kinds`, `At`, ordered `Items` and
`Warnings`; each item has `Action`, `Resource`, `Key`, `Source`, sorted
`Changed` names and optional `RecordID`. No password, rich-text body, file
bytes or before/after field value is exposed. `Apply` reports the actions it
performed inside its caller's transaction; on error it returns no success
plan or stale row. Its `Result` is provisional until that caller commits;
`RunCommand` returns only after its transaction commits, and a caller may
surface a generated password only then.
`UNCHANGED` includes no write, no timestamp change, no outbox event and no
audit record. A warning is not counted as a write. Dry-run writes nothing.

Every refusal is a typed `Problem` with `Code`, `Class` (`correctable` or
`immutable`), `Source`, optional `TargetSource`, and the owner's underlying
problem text; formatting names `file:line:column` and both ends of a reference.
`correctable` means a seed edit, grant, prerequisite or retry can fix it;
`immutable` means the selected tenant/build cannot perform the requested
operation. The following are exhaustive for the seed layer; an owner may add
its own typed refusal, which is preserved with the same source wrapper.

| Refusal | Class | No-write rule |
| --- | --- | --- |
| Malformed YAML/JSON, duplicate/unknown key, unsafe path, password in file, invalid date or unsupported v1 field | Correctable | Before transaction writes. |
| Duplicate resource/key across kinds, missing writer, unsupported command or translation-only record with no writer | Correctable | Before transaction writes; wire/upgrade or edit. |
| Dangling reference or cycle | Correctable | Before transaction writes; report both ends. |
| Demo requested for a tenant whose persisted `Demo` is false | Immutable | Refuse even in dry-run; no downgrade or `--demo` override. |
| Missing, foreign or unauthorized seed permit | Correctable | No writer is entered; use a trusted tenant-create or operator command entry point. |
| Missing operator/resource grant or unavailable owner prerequisite | Correctable | Recheck in the authoritative tenant transaction. |
| Owner validation, policy, last-one-away, revision conflict or occupied natural key | Correctable | Owner refusal text survives; whole run rolls back. |
| Tenant absent, suspended or wrong client composition | Correctable | Select/create/resume the right tenant or client before retrying; no writer runs. |
| Database, storage or event publication failure | Correctable | Roll back tenant rows, outbox events and key mappings; file bytes may need the existing orphan sweep. |

## Ordering, transaction and concurrency

1. Load both requested kinds, reject duplicate keys, and preflight source
   grammar and assets before a mutation. `starter` is always selected. `demo`
   is selected only when `Selection.Demo` and the tenant row's immutable
   `Demo=true`; a non-demo tenant refuses rather than silently skipping demo.
2. A node is one record. A typed reference from a record to another is an edge
   from dependency to dependent. The kernel validates every referenced alias
   and target in the same tenant, and topologically sorts by dependency, then
   by resource alias and UTF-8 key for stable ties. A cycle reports the
   closing edge's source and target locations. A writer may declare command
   dependencies (roles before user role grants, user before task assignment).
3. One `db.Tx[db.Tenant]` encloses the **whole selected run**, including
   `seed_keys`, all owner rows and their outbox events. This is stronger than
   per-resource atomicity: a late validation failure rolls all resources
   back. Create/update commands run in graph order; stale-record deletions run
   in reverse order of the writers' declared reference dependencies, then by
   resource/key for stable ties. The file no longer contains a removed record,
   so no record-level edge can be inferred for it; any remaining relationship
   is checked by the owner delete command. A refused prune rolls back new
   writes too.
4. `Apply` takes one per-tenant transaction advisory lock before its first
   read, so two seed runs serialize. It then reads each target row `FOR UPDATE`,
   rechecks actor grants, tenant, owner revision (when present) and desired
   state, and invokes the owner path only for a real difference. A human write
   is serialized by the row lock or rejected by the owner's expected-revision
   check. A concurrent insert on an unowned natural key may cause a unique
   conflict; the whole run refuses and a caller may retry from a fresh read.
   No stale `Plan` is applied. This checkout's content/task/user Specs have no
   revision API; they use row locking until T-0138's owner revision exists.
5. `seed_keys` is a kernel-owned table, one transactional
   `000051_seed_keys.up.sql` migration (no down file), with
   `(tenant_id,module,entity,key)` as primary key, `kind` and nullable `record_id` as
   read fields, `tenant_id NOT NULL`, `ENABLE` and `FORCE ROW LEVEL SECURITY`,
   and the same `platformkit_tenant_match(tenant_id)` `USING`/`WITH CHECK`
   policy as `contents`. Every row created or changed by seed gets a mapping,
   including natural keys, because prune reads provenance; `key` holds the
   identity the owner stores (`Resource.CanonicalKey`), so two spellings of one
   natural key are one mapping rather than two licences to write or delete one
   row. An identical
   pre-existing unowned row gets no mapping or write. Lookup prefers a mapping's record ID
   when the owner has a UUID identity; natural-key owners without one (auth
   roles) keep `record_id=NULL` and resolve by the owner's key. An absent
   mapping resolves via the owner's natural key, or means absent for a
   keyless resource. A mapping whose row is gone may be replaced only after
   the owner read confirms absence in this tenant. The table is never a source
   of business fields.
6. The existing tenant `OnCreate` hook receives `db.Tx[db.System]`; calling
   `db.Run` inside it currently returns `ErrScopeMismatch`. Add a `kit/db`
   `InTenant(ctx, tx db.Tx[db.System], tenant tenancy.Tenant,
   fn func(context.Context, db.Tx[db.Tenant]) error) error` bridge that takes
   that system handle and the newly created tenant,
   temporarily sets `platformkit.system_access=false` and
   `platformkit.tenant_id=<id>` on the **same** PostgreSQL transaction, yields
   only `db.Tx[db.Tenant]`, and restores the system settings before returning.
   Nested `db.Run` joins the tenant scope; nested system access is refused.
   A failed set/restore or panic aborts the outer tenant creation transaction.
   The bridge may be used only while the system handle is active, never via
   `db.Detached`. `seedRoles` runs before this hook until the role writer
   replaces it; then the literal hook list contains just the seed hook. Extend
   `tenant.Hook` and the tenant create result to carry an optional generated
   demo password in memory to the caller, without a database column: the
   operator-only HTTP create handler returns it once after `RunSystem` commits,
   and bootstrap/CLI prints it once after commit. A failed transaction returns
   no secret. The existing `Hook` return of only `error` cannot fulfill that
   result contract.
7. The CLI selects its explicit client composition and calls `RunCommand`.
   That runner opens one system transaction, resolves the operator tenant and
   `--tenant <host>` with the existing tenant service, enters the operator
   tenant through the typed bridge to verify the credential and
   `tenant:manage`, restores system scope, then enters the target tenant
   through the bridge and calls `Plan` or `Apply`. Both grant checks happen
   after row lookup in the same authoritative transaction; revocation before
   that lookup is seen. The reference binary's command is
   `platformkit seed --client platformkit --tenant <host> [--demo] [--dry-run]`;
   a downstream flagship owns the same flags and its literal client switch.
   `make seed CLIENT=<slug>` is a thin invocation of that command; no server
   is started. A tenant creation hook reads `NewTenant.Demo`, persisted on
   `Tenant` by a transactional `000050_tenant_demo.up.sql` migration and carried
   by `tenancy.Tenant`; only creation sets it. Starter applies for every
   tenant, demo only when that flag is true.

   **Delivered (T-0194), differently and deliberately.** No provisioning permit
   exists, and the tenant `Hook` signature and create result are unchanged. The
   creation hook (`seedProvisioner` in `apps/platformkit/seed.go`) calls
   `seed.Service.ApplyProvisioned`, whose authority is state rechecked in the
   create transaction rather than a minted token: the tenant holds no `seed_keys`
   row, every record the selected files declare is absent, and — checked by the
   hook, about the table the seed does not own — the tenant holds no person. That
   answers the objection this section was written to answer (an operator holding
   `tenant:manage` and not `content:manage` must still be able to create a tenant)
   without changing who may create one: the run is not authorised as a person's,
   it is confined to a tenant that came into being in the same transaction and can
   only create. The demo password is config's `demo.password`
   (`PLATFORMKIT_DEMO_PASSWORD`), not a value the hook returns to its caller; no
   generated secret travels through a create result, because `tenant.Hook` returns
   only an error and this delivery does not widen a contract owned by another task.
   A deployment that names none gets one minted per person by the seed's own
   `SetPassword` call. Where that minted value goes is decided by the run, not by
   the writer: a command collects it and prints it once after its commit, and a
   creation hook, which has no caller to hand it to, keeps the hash and prints
   nothing — see the demo-password paragraph under *Files and grammar* and
   `apps/platformkit/seed_credentials.go`. What this shape refuses to do is write a
   minted credential to the process's output, which inside a running server is its
   log.

## Time, audit and external effects

The injected clock supplies one UTC instant per run. `+3d` and `-2y` use
calendar addition (clamp an invalid day to that month's last day); date-only
fields start from the UTC date and timestamp fields from the UTC instant.
`monday 09:00` means the first Monday at 09:00 UTC **at or after** that instant.
Only writer-declared date/timestamp fields accept these full-string forms; the
writer emits UTC ISO 8601 values to the owner's write path. No tenant locale or
process timezone changes the result. Money, if a writer has any, remains
`int64` minor units plus a Currency and is never parsed from a price string.
At a pinned clock of 2026-10-01 10:30 UTC, a timestamp `+3d` is
2026-10-04T10:30:00Z, a date `-2y` is 2024-10-01, and `monday 09:00` is
2026-10-05T09:00:00Z.

A relative date is read once, by the run that **creates** the record, and is not a
value any later run reconciles (`Target.CreateOnly`; `kit/seed/README.md` names the
rule). A declaration of `+3d` therefore gives the task a deadline three days after
the tenant was seeded and then leaves it where a person can move it — a field
whose declared value moves with every clock would report an update on every run
forever, which is the opposite of this contract's idempotence rule.

An event published during a seeded owner write carries the same W3C trace
context as the run. The command creates one if none was received. Extend the
existing `kit/events` context API with
`WithAttribution(ctx, Attribution{ActorKind: "seed", SourceFile, SourceLine,
InitiatorID})`; `Apply` puts it around each owner write. Extend the
existing outbox/event envelope with a bounded attribution of `actorKind=seed`,
`sourceFile`, `sourceLine` and optional operator `initiatorID`, and have audit's existing all-event subscriber
persist/read those fields and the trace ID. The UUID actor remains null for a
system seed, never a fabricated user UUID; the audit's actor label is `seed`.
Each owner event therefore produces an audit row with tenant, event ID, seed
source and trace after delivery. The row and outbox event commit together;
`seed_keys` changes only in the same transaction as an owner write/event, never
on an unchanged row, so its provenance change is covered by that event's
source-stamped audit record. The audit subscriber is asynchronous and
at-least-once, so the audit row is
eventually present, not present at the instant `Apply` returns. No seed event
duplicates an owner's event. A refused mutation emits no domain event and no
seed audit row. Event payloads and plans never contain demo passwords.

File storage is outside PostgreSQL: upload bytes may remain orphaned when the
transaction rolls back, for the existing sweep to remove. Embedded local assets
are bounded by the file module's configured limit; its upload runs while the
seed transaction is open, so a large allowed asset can hold a connection for
that copy's duration. Email triggered by
`user.Invite` is an ordinary post-commit side effect; a refused run leaves no
invitation event to deliver. Provider credentials, delivery behavior and asset
licensing are product decisions.

**Delivered (T-0194).** The outbox, the envelope and the audit trail all carry
the attribution. `kit/events.WithAttribution` puts an `Attribution`
(`ActorKind`, `SourceFile`, `SourceLine`, `InitiatorID`) on the run's context,
`Apply` puts it around each owner write,
`migrations/000052_outbox_attribution.up.sql` stores the four beside `actor`, and
the relay carries them onto the CloudEvents
envelope (`actorkind`, `sourcefile`, `sourceline`, `initiator`). A
`platformkit seed` run mints one W3C trace, so `traceparent` names the run for
every row it caused. `audit.Service.Record` copies the four into `audit_events`
(`modules/audit/migrations/000053_audit_attribution.up.sql`), so the trail
outlives the outbox row the relay deletes: a seeded write is labelled `seed`
between the file and line that asked for it, with no actor because nobody
signed in, and its initiator beside it. The two read routes return them.

The schema changes this design proposed, verbatim, were a transactional kernel
`000032_outbox_seed_attribution.up.sql` (nullable actor kind, source file/line
and initiator UUID on `platformkit_outbox`) and a transactional audit-owned
`modules/audit/migrations/000024_seed_attribution.up.sql` (those read fields
plus `traceparent` on `audit_events`). Existing rows keep null attribution.
The relay carries the new fields in the existing CloudEvents envelope; audit
reads them from that envelope, and its API returns them. These are additions
to existing RLS-protected tables, with no down files or second audit store.

## Conformance cases fixed before implementation

The fake is `fakeWriter` in `kit/seed/service_test.go`: a small map-backed
implementation of the port. It
holds separate tenant maps, snapshots and event/audit intentions; its mutation
methods call the same `Decide` as the SQL
adapter. The package's private reconciliation engine accepts a narrow key
store: the production adapter reads/writes `seed_keys`, and the fake harness
uses a map and stages its changes until the case succeeds. This lets the
cases exercise ordering and no-write refusals without manufacturing a
`db.Tx[db.Tenant]`; the public `Apply` still requires that type and takes the
Postgres lock before entering the same engine. The fake does **not** claim to
prove RLS, a database commit, or file-storage rollback. The executable cases
below sit under `kit/seed` and run against the fake; the SQL adapter runs the
same decision cases plus the database cases. The names in the table below are the intended set, not the
delivered one. The delivered cases are
`kit/seed/{seed,service,keys,demo_row,prune_gone_row}_test.go` — the loader,
the ordering and the decision engine, the key table's RLS, the tenant row's
answer, and a prune whose row is already gone — and, in `apps/platformkit`,
`seed_owners_test.go` (the write path, the provisioning proof, the owner's
refusal and the command), `reference_seed_test.go`, `seeded_home_test.go`,
`seed_attribution_test.go`, `seed_audit_trail_test.go` and
`seed_concurrent_runs_test.go`. Each is named for the behaviour it pins rather
than for a row of this table. `kit/seed/kind_references_test.go` and
`kit/seed/kind_move_prune_test.go` cover one resource declared in two kinds —
its references and its provenance — and `seed_revoked_actor_grants_test.go`,
`seed_task_title_update_test.go`, `seed_slug_normalization_test.go` and
`seed_task_natural_key_refusal_test.go` cover the
grant re-read against a revocation that committed mid-run, an edited title on the
row the file already wrote, the owner's canonical slug, and the person's own task
that a task key names but a run will neither adopt nor edit.

| Case name | Fake setup and action | Required result |
| --- | --- | --- |
| `TestSeedCreatesThenConverges` | Empty tenant A; apply the same starter content twice. | First create, second unchanged; one event intention; same ID and timestamp. |
| `TestSeedEditsOnlyManagedFields` | Existing page with server timestamp and unrelated human field; change seeded title. | One update of title; unrelated field intact; next run unchanged. |
| `TestSeedCommandStateUsesOwner` | Draft content with publish command, then published content. | First apply calls publish once; second calls nothing. |
| `TestSeedNaturalAndOpaqueKeys` | Existing identical unowned email row and missing keyless task. | Email resolves as unchanged with no mapping; task gets mapping; both rerun unchanged. A differing unowned email row refuses. |
| `TestSeedPruneIsOptIn` | Seeded task removed from file, then `prune: true`. | No deletion before flag; owner delete once after flag; mapping removed with it. |
| `TestSeedReferencesAndCycle` | User, assigned task, image page, then a two-node cycle and missing target. | Topological IDs on success; each refusal names both source and target locations and calls no writer. |
| `TestSeedDemoRequiresFlag` | Tenant A `Demo=false`, tenant B `Demo=true`; request demo for both. | A refuses immutable with no writes; B applies; starter applies to either. |
| `TestSeedTenantSeparation` | Same source keys in A/B with distinct snapshots. | Distinct IDs, mappings and events; A reads no B key. Fake checks routing; Postgres RLS test proves isolation. |
| `TestSeedRelativeDatesUseClock` | Clock fixed at 2026-10-01 10:30 UTC. | `+3d` is 2026-10-04 10:30 UTC for a timestamp; next Monday 09:00 is 2026-10-05 09:00 UTC; same clock rerun unchanged. |
| `TestSeedOwnerRefusalRollsBack` | Later writer returns missing-required-field, bad enum or refused rich text. | Source and owner text returned; transaction harness discards earlier intentions; no success plan. SQL test proves actual rollback. |
| `TestSeedRevisionAndGrantRechecked` | Change revision or revoke grant after dry run. | Apply refuses, writes/emits nothing; caller may plan again. |
| `TestSeedLastAdministrator` | Prune would remove the last administering person. | Owner refusal; prior state and events remain. |
| `TestSeedTranslationWarning` | No translation writer, `i18n.pt` present. | Base write succeeds; one warning; no pretend translation. |
| `TestSeedPasswordNeverLeaks` | User creation with generated password. | Password absent from plan, event and audit intention; rerun never calls `SetPassword`. |

What Postgres conformance covers, and where:

- The typed system-to-tenant bridge, and the rollback of owner rows, outbox
  events and key mappings: every case in `apps/platformkit/seed_owners_test.go`
  opens `dbtest.System` and `db.InTenant` over the composition's real owners,
  and `TestTheSeedRefusesARecordItsOwnerWouldRefuse` reads the refused record
  back to show a refused run left nothing behind.
- RLS on `seed_keys`: `TestSeedKeysAreIsolatedByDatabaseRLS` in
  `kit/seed/keys_test.go`, under the application role whose policies the
  migration FORCEs.
- Two seed runs on one tenant: `TestTwoSeedRunsOnOneTenantCreateEachRecordOnce`
  in `apps/platformkit/seed_concurrent_runs_test.go`. It holds the first run's
  transaction open while the second starts, and it holds only while `lockRun`
  takes the per-tenant advisory lock — with that call removed, the second run
  fails on the `contents_tenant_slug` unique index instead of waiting.
- Audit delivery with trace and seed source:
  `TestSeededWriteCarriesSourceActorAndTrace` and
  `TestASeededWriteIsAuditedAsTheSeedWithItsSource` in
  `apps/platformkit/{seed_attribution,seed_audit_trail}_test.go`.
- The browser journey: `e2e/site.spec.ts`'s "a new tenant opens on its starter
  home, and a published page takes its place", with
  `TestSeededTenantOpensOnItsHomePage` as the same claim without a browser.
- Key identity, which is the owner's spelling: `contents` and `users` declare
  `CanonicalKey`, and four cases hold it. `TestSiteHomeReferenceMeetsTheOwnersSpellingOfTheSlug`
  (`apps/platformkit/seed_site_home_canonical_slug_test.go`) opens the site on the
  slug the page's owner stored and reruns unchanged;
  `TestSeededPersonMeetsTheOwnersSpellingOfTheirAddress`
  (`apps/platformkit/seed_user_email_canonical_test.go`) seeds `Person@Example.test`
  as the one person the user module stores and emits no second event;
  `TestRespellingAPagesKeyToItsSlugDoesNotPruneIt`
  (`apps/platformkit/seed_slug_respelled_prune_test.go`) proves a pruning file that
  respells a key keeps the page; `TestSeededPersonIsProvenancedUnderTheAddressTheModuleStores`
  (`apps/platformkit/seed_user_mapping_spelling_test.go`) reads `seed_keys` and asks
  that one person holds one mapping, written in the module's spelling;
  `TestPruneKeepsARowItsOlderMappingSpellsDifferently`
  (`apps/platformkit/seed_legacy_mapping_spelling_prune_test.go`) leaves a row whose
  provenance predates its writer's declaration standing;
  `TestTwoSpellingsOfOneIdentityRefuseAtTheSecondDeclaration`
  (`kit/seed/duplicate_identity_test.go`) refuses two declarations that fold to one
  record and writes nothing. `TestEverySnapshotFieldAWriterFillsIsReadByTheService`
  (`kit/seed/snapshot_fields_read_test.go`) reads the port's own source and refuses
  any `Snapshot` field the service never consults.

Two of the items this contract first asked for are **not delivered here**. A
manual-write revision race has no owner to race against yet: no Spec in this
checkout takes an expected revision, so a run serializes on the row locks
`crud.GetForUpdate` takes until T-0138 lands one (see *Ordering, transaction and
concurrency* above). And file orphan cleanup has nothing to sweep: a seed run
uploads its one demo asset and writes no other file bytes —
`apps/platformkit/seed.go` names five writers (content, site, user, task and
file) and the starter and demo files declare pages, one site record, people's
roles, three pieces of work and one image.

## Ten module questions and limits

1. **Entity fields:** the kernel owns only `seed_keys` identity/provenance and
   transient `Record`/`Plan` values. Business fields remain with each writer's
   owner; unknown or read-only fields refuse (correctable).
2. **Identity and relationships:** natural keys use owner normalization; opaque
   keys use `seed_keys`. Typed aliases/references resolve under one tenant Tx;
   dangling references and cycles refuse (correctable).
3. **States and transitions:** seed plans have create/update/unchanged/prune;
   domain transitions stay in owner commands. Invalid transitions refuse with
   owner text (correctable).
4. **Authorization:** composition names writers and grants; `Authorizer.Check`
   rechecks each action in the authoritative transaction. A non-demo tenant's
   demo refusal is immutable for that tenant.
5. **Transaction and concurrency:** one tenant transaction for the selected
   run, seed advisory lock first, row locks and optional revisions, full
   rollback on refusal. Unique-key races refuse for retry (correctable).
6. **Events and idempotency:** only changed owner commands publish through the
   outbox. `Decide` uses normalized managed state; event ID supplies delivery
   idempotency and `seed_keys` supplies stable key mapping.
7. **Retention and export:** `seed_keys` has no TTL; it lives while the
   seed-owned row does, and prune removes it with that row. This checkout has
   no tenant-delete path; any future tenant deletion must remove it with the
   owner's rows.
   A dry-run plan is exportable text/JSON without sensitive values. Domain
   export and audit retention stay with their owners.
8. **Provider integration:** embedded `fs.FS` supplies files; `file.Service`
   uploads, events deliver effects. Provider outage refuses (correctable);
   no direct blob or business-table SQL.
9. **Locale, time and money:** injected UTC clock and fixed relative-date
   grammar; i18n uses T-0190 when present or warns. Money stays typed as minor
   units/currency in its owner.
10. **Specialist validation:** the owner's Spec/service validates fields,
    commands, policy, rich text and files. Seed does only syntax, graph,
    source and reference validation; an owner refusal is preserved
    (correctable unless that owner classifies its invariant immutable).

**Limits left to the product:** choose actual page copy, demo people, role
grants, file assets and licenses, translation content and provider
configuration. The reference CLI authenticates an operator using email and
password from `PLATFORMKIT_SEED_OPERATOR_EMAIL` and
`PLATFORMKIT_SEED_OPERATOR_PASSWORD`, never argv; its composition checks the
credential against the operator tenant's user and role services, and rechecks
`tenant:manage` before entering the target tenant scope in the same system
transaction. A downstream flagship must supply equivalent operator
authentication; a shell account's ability to read a database URL alone is not
a seed grant. The kernel cannot choose a client
slug, a sector fact, a password, a retention period or which resources a
product composes. The reference app fixture is specified as two starter pages,
five demo pages (one with an embedded image and one with Portuguese `i18n`
waiting for T-0190), three demo users and three demo tasks; their names and
copy are product content. Its composition validates before tenant-create
commit that its `admin` role can administer roles and that only the operator
tenant's admin role gets operator grants; the generic seeder never chooses
those grants. `platformkit-mobile` needs no seed implementation: it reads
the same tenant rows via existing APIs; a seeded-content mobile journey is a
named follow-up once a mobile consumer is wired. `tools/pillars.py` is absent
from this resolved checkout, so no before/after indicator can honestly be
claimed. This specification changes no runtime number.
