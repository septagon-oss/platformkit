# Kernel schema

`migrations/` is the kernel's own schema, owned as `platformkit`: the tenancy
helpers and the tenants and hosts they resolve, the outbox with its claims and
dead letters, and the limits ledger. What a module stores lives beside that
module under `modules/<name>/migrations`, and `kit/app` hands the kernel's
source to the runner first with the selected modules after it. Files keep the
numbers they were applied under, so the gap above `000021` belongs to the
modules that took those versions with their own SQL.

Two names carry this directory's contract to the outside, and both are
specified where they are defined rather than restated in prose:

- **`platformkit_module_schema(owner text)`** is the one line a module's first
  schema revision runs. It creates the schema named by that module's migration
  owner and opens it to the roles the deployment already hands the objects it
  creates beside that schema: `USAGE` on the schema, and then, for each of them,
  the privileges that deployment pinned for it by default — read out of the same
  `pg_default_acl` row that named the grantee, so a role the deployment pinned to
  `SELECT` reads a module's tables and does not write them. The rows it reads are
  the ones pinned for the namespace the migration runs in
  (`defaclnamespace = current_schema()`), because PostgreSQL scopes a default
  privilege pinned `IN SCHEMA` to that namespace and to no other: a deployment
  that pins `SELECT` for a role in one namespace and `INSERT` for the same role
  in a second leaves it reading the first and writing the second, and a module
  schema that unioned those two rows would hand that role a write the deployment
  refused it everywhere. Defaults pinned database-wide (`defaclnamespace = 0`) are
  not mirrored and do not need to be — such a row already applies to every object
  the role creates, in a schema created after the pin included — but opening a
  namespace is not a privilege on a table, so a deployment whose pins are all
  database-wide finds its module schemas shut (see the first consequence below,
  and `TestModuleSchemaMirrorsNoDatabaseWideDefault`). It discovers grantees *and*
  privileges rather than naming either, because a module's SQL may not know, or
  care, which role its deployment runs beside, and because the privileges are
  the deployment's to decide: a role that must write a module's tables is a role
  its deployment pins write defaults for in the namespace a module's schema opens
  beside, and no module schema hands out more than that. Its own header —
  [`000026_module_schema.up.sql`](000026_module_schema.up.sql) — carries the
  refusals, why it sets no `search_path`, and what a module's next revisions
  look like.
- **`dbtest.TenantTablesSQL`** ([`kit/db/dbtest`](../kit/db/dbtest/tenanttables.go))
  is the tenant-scope walk: every ordinary table in the current schema *and* in
  every schema whose name is an owner in `schema_migrations`, with RLS enabled
  and forced, the table's comment and its policies' `USING` expressions. This
  repository's [`rls_test.go`](rls_test.go) asserts over the constant, and a
  module's schema is inside the walk because the ledger says which schemas are
  modules', not because a list does.

Four consequences belong to the deployment rather than to the kernel:

- A deployment whose migration role holds no default privileges for the namespace
  the migration runs in — none at all, or only database-wide ones, which are not
  that namespace's rows — yields zero grantees, and the schema the function opens
  is then readable by its owner alone. That is what such a deployment asked for:
  the defaults it pinned apply to a module's tables where they land, and it pinned
  no rights to a namespace anywhere, which is the one thing a schema needs. From
  outside it is indistinguishable from a broken installation, so read a module's
  first table as the application role before anything depends on it.
- A role pinned for that namespace is half admitted to it until the deployment
  grants it `USAGE` on the namespace as well, which is what
  [`apps/platformkit/postgres-init.sql`](../apps/platformkit/postgres-init.sql)
  does for `platformkit_app` in the same file, before the pins that name the role.
  The function hands `USAGE` on the schema it opens to every grantee the
  namespace's rows name, and it does not ask, before handing it, whether that
  grantee may open the namespace the rows were read from — measured, such a role
  holds `USAGE` on the module schema and not on the namespace the kernel's SQL
  lives in. What follows is not a refusal that names the missing grant: PostgreSQL
  drops a search-path element the current role cannot use, so the unqualified
  `platformkit_is_system()` inside the policy predicate `platformkit_tenant_match`
  resolves to nothing, and that role's first read of a module's table answers
  `function platformkit_is_system() does not exist` (SQLSTATE 42883) — a message
  naming a function rather than the grant. Grant the `USAGE` beside the pins, as
  the reference deployment does, and the same read answers zero rows outside a
  tenant transaction and one row inside the right one.
- A deployment that pins its default privileges to `PUBLIC` in the namespace a
  module's schema opens beside gets module schemas every role in the cluster can
  open, with the defaults it pinned on their tables. The function discovers the
  grantee the catalog spells `0` the way it discovers any other and hands it
  exactly what its own row said, which is that deployment's choice executed rather
  than a kernel decision; row-level security still binds every read to a tenant,
  and nothing binds it to a role. Defaults meant for one role belong pinned to
  that role.
  `TestModuleSchemaDiscoversPublicAsAGrantee` keeps the decode itself honest.
- `CREATE SCHEMA` needs `CREATE` on the database. A migration run by the
  database owner has it; a dedicated migration role has to be granted it, or
  every call fails for a reason that has nothing to do with this function.
  [`apps/platformkit/postgres-init.sql`](../apps/platformkit/postgres-init.sql)
  is the reference deployment's own answer.

### Built on what came before

Decision 0022 asks a delivery to name what it composed rather than what it
rebuilt. **Reused:** the function's three kinds of statement are the ones
[`apps/platformkit/postgres-init.sql`](../apps/platformkit/postgres-init.sql)
already runs for `public` and `dbtest.URLsFor` already runs for the schema it
makes for one test, and its grantees are decoded by the same `aclexplode` and
the same `grantee = 0` case as the runner's `migrationLedger` block; its tests
compose `dbtest.URLs`, `db.Migrate` over `fstest.MapFS` sources, the table
shape `000001_tenancy.up.sql` documents and the sequence-backed column one
`GRANT … ON SEQUENCES` statement is for. **Added:** the function, because no
unit in either repository opened a schema to a role it discovered rather than
named, and the walk's second arm, because a table in a module's schema was a
table nothing checked. **Made reusable:** the two names here an importer can
reach — `dbtest.TenantTablesSQL`, the one walk, which replaces the catalog's
second copy of that query once its pin moves, and `dbtest.DeploymentSchema`,
which asks the server which namespace an unqualified statement lands in and gets
back the schema this package made for the test — the one name an owner name is
cut from. It reads `current_schema()` and not `current_setting('search_path')`
because a setting is a list, and the name a caller cuts an owner out of has to
be one namespace. The claim to make about the second one is narrow: it is what a
test written *here* calls, and `moduleOwner` and the tests beside it are built on
it. `review_round1_module_schema_gaps_test.go` and
`review_round2_module_schema_grants_test.go` read `current_setting('search_path')`
and re-assert the shape themselves, because a file a review contributed is
committed as received and the second of them arrived after the helper did — two
copies that exist, disclosed, rather than a clause that says none do.
`tenantScope`, which reports the tables a walk listed separately from the problems it found so a walk that saw
nothing cannot pass, and `openSchema`/`tenantTable`, the fixture pair a module's
first two revisions look like, are unexported helpers of
`package migrations_test`, which no importer can reach: reusable as the shape a
later test file copies, not as an API.

# Writing a migration

Every `.up.sql` file under a `migrations/` directory — the kernel's here, and a
module's under `modules/<name>/migrations/` — is forward-only, immutable once
applied, and owned by one capability. [ADR 0011](../docs/adr/0011-migration-ownership.md)
owns that contract; this file is the part an author reads: what a file may say
about itself, and what the runner refuses.

A file runs in one transaction with its history row. Three kinds of file are not
like that, and a file has to say which it is, because a reader cannot tell from
the SQL.

## The header

A run of comment lines at the very top of the file, before any SQL:

```sql
-- pkit: phase=data
-- pkit: batch=5000
-- pkit: table=billing_plans
UPDATE billing_plans SET currency = 'EUR'
  WHERE currency IS NULL AND id IN (SELECT id FROM batch)
```

(The table is whatever the file drains; the header's job is to say which, in a form
the runner can read before it connects. The body sees one window of that table's
primary key as the relation `batch`, and the runner runs it once per window, in its
own transaction.)

A body that never reads that relation cannot be bounded by it, so it is refused by
`data-body-unbounded` unless it says why it bounds itself. The decision whether to
wrap a body is made from the same reading of the same text as that refusal — comments
gone, case folded — so the file the guard judged is the file that runs: an excepted
body runs once, and a body that names `BATCH` in another case still gets its window.

A file that keeps a statement the rules refuse says so, in the sentence a reviewer
will read with the marker:

```sql
-- pkit: allow=index-not-concurrent reason=billing_plans holds one row per tenant
CREATE INDEX billing_plans_currency ON billing_plans (currency)
```

Each line is `-- pkit: key=value [key=value …]`. `reason=` runs to the end of its
line; every other value is one word. A key may not repeat. After the first line
that is not a header line, a `-- pkit:` marker may not appear again — a marker the
runner would not read claims a review the runner never did. The checksum covers
the whole file including the header, so an applied file can never be marked:
marking it would mean changing bytes some installation already ran.

| key | values | who reads it |
| --- | --- | --- |
| `phase` | `expand` (the default), `contract`, `data` | the runner, for the order a release may apply in and how the file runs |
| `expand` | a version of the same owner | required by `phase=contract`; the expansion this file waits for |
| `batch` | rows, 1…100000 | required by `phase=data`; one transaction per window |
| `table` | one bare lower-case identifier | required by `phase=data`; what the window walks |
| `autocommit` | `true` | the file's one statement runs with no transaction around it |
| `allow` | a rule name below | excepts that rule for this file; needs `reason=` on the same line |
| `reason` | a sentence, at least three characters | nobody but the reviewer — that is its function |

A grammar mistake refuses before the runner connects, and no `allow=` excuses one:
an exception that can except a broken marker is a marker nobody can rely on.
`batch` is refused for the value the file wrote — `batch=0` is refused as `batch=0`,
not as a missing key — because a refusal that names the wrong key sends the operator
to a line that is already there.

## What the runner refuses, and what to write instead

Each refusal names its rule, says what the file does, and says what to do instead.
The engine reads text with comments stripped, not a parse tree — the runner is not
a SQL parser — so a statement inside a dollar-quoted body can be flagged, and the
answer is the marker.

| rule | fires on | why | exception |
| --- | --- | --- | --- |
| `alter-column-type` | `ALTER [COLUMN] … TYPE` or `… SET DATA TYPE` | a full rewrite under `ACCESS EXCLUSIVE`; the running application stops for the length of the table | allowed |
| `add-column-not-null` | `ADD COLUMN … NOT NULL` with no `DEFAULT` on that column | rewrites the table and refuses every write while it does | allowed |
| `index-not-concurrent` | `CREATE INDEX` without `CONCURRENTLY` on a table this file does not create | the plain build takes a `SHARE` lock that stops every writer for the length of the build | allowed |
| `drop-column` | `DROP COLUMN` in a file that is not `phase=contract` | it takes a name away from the release running right now | allowed for a column no installation had rows in |
| `index-concurrent-without-autocommit` | `CONCURRENTLY` without `autocommit=true` | PostgreSQL refuses the statement inside the runner's transaction (`25001`) | none: add `autocommit=true` |
| `autocommit-without-concurrently` | `autocommit=true` with nothing nontransactional in the file | the marker gives up all-or-nothing; nothing may do that without a reason | none: delete the marker |
| `autocommit-not-rerunnable` | an autocommit `CREATE INDEX CONCURRENTLY` without `IF NOT EXISTS`, or a `DROP INDEX CONCURRENTLY` without `IF EXISTS` | the statement can succeed while the version stays unapplied, so the next run must be able to repeat it | none |
| `data-body-unbounded` | a `phase=data` body that never reads the `batch` window | the window cannot bound it, so one statement walks the whole table | allowed, with the sentence saying how it bounds itself |
| `data-with-ddl` | DDL in a `phase=data` file | that file runs outside a transaction, in pieces; DDL there has no rollback | none: split the file |
| `data-writes-outbox` | `platformkit_outbox` named in a `phase=data` body | one event per row per attempt buries the relay and replays on a resume | allowed |
| `unused-allow` | an `allow=` for a rule the file does not break | an exception nobody needed is a claim about a risk that is not there, and it outlives the sentence that justified it | none: delete the marker |

The rules whose exception column says `none` have no marker, and an `allow=` naming
one is refused as what it is — a bypass with a rule name on it — rather than silently
switching the rule off. Each of them states something a marker cannot make false: what
PostgreSQL refuses, what the autocommit mode costs, or what a data file cannot
survive.

The rule about a type change reads the clause inside an `ALTER TABLE`, not the word
`COLUMN`, because PostgreSQL makes that keyword optional and both spellings are the
same rewrite. The rule about a `NOT NULL` column reads one column definition at a time
for the same reason: the `DEFAULT` that makes the statement ordinary has to be that
column's own, so a `SET DEFAULT` in a statement beside it excuses nothing — and so does
the `IS NOT NULL` a partial index carries in its `WHERE` clause, which is a predicate
over rows and not a constraint this file adds. `ADD COLUMN a integer NOT NULL, ADD
COLUMN b integer DEFAULT 0` is refused for `a` whatever `b` says.

Two rules about a release rather than a file: a `phase=contract` file refuses while
its `expand=` version is not already in the installation's history, and nothing of
an owner applies past a `phase=data` file that has not finished draining — the
worker drains that, and the next migration continues. Two drains are the run's own: one
already in flight, which `Migrate` takes up under the bound a migration gives itself
(past that bound the boot continues and the tick finishes it), and the owner's last
pending data file, which has nothing behind it waiting on the work and a window to bound
it by. A body that said it bounds itself has no window, so it stays the worker's even
last. An owner with no history at all is the exception both times: nobody is reading,
and its files apply in order.

## The floor: guards apply to new versions

A rule cannot be refused on a file that is already applied somewhere: the bytes are
immutable and the only remedy left would be to stop the installation. So a source
states the first version it is guarded from — `RulesFrom` on its
`db.MigrationSource`, which a module also carries on its manifest — and the number
is one past the highest file the rules refuse today. In this repository the floors
are measured, not chosen: `platformkit` 21, `audit` 24, `auth` 14, `user` 26. A
source that says nothing is guarded from version 1, which is what a module added
after these rules exist should declare. Lowering a floor is a review, not an edit.

A floor is bounded by the history it can claim. The versions a source can point at end
at its own highest file — `pendingMigrations` refuses an applied version a release no
longer lists — so a number past that head plus one excuses nothing that exists: it is
the guard switched off for versions nobody has written yet, the opposite of what the
field is for. Such a floor earns no exemption, and every file of that source is judged
at the point where the ledger says which of them are pending (a file below an *honest*
floor is applied bytes, and those the rule table must not judge).
`kit/db/review3_guard_floor_test.go` holds both directions of that, and a contract half
behind the same unusable floor still waits for its expand.

## The retry, and the rehearsal

A file that came back `contended` applied nothing and may be run again by whoever
chooses to wait. The door for that is `platformkit migrate --config config.yaml`:
every role's own migration over the same composition, sources, floors and budgets,
without a server attached. `--drain` adds the rest of the convergence — the backfill
the migration deliberately left to the worker, then the migration again for the files
that waited behind it — which is what a deployment does across two ticks of its
worker anyway.

What no guard or budget can say is what a release will cost against the table the
installation actually has. `make rehearse` is the step that answers it, and the step
a release requires before a version is published:

```sh
REHEARSE_ARGS="--base-ref v1.1.0" make rehearse                  # the previous release's own schema
./scripts/rehearse_migrations.sh --dump /backups/last-night.dump # somebody's real database
```

It builds this tree, makes a copy of a production-shaped database — the previous
release's own binary migrating a fresh one, then ten thousand rows per seeded table
(`scripts/testdata/rehearse/seed.sql`), or an operator's dump restored into it — and
runs `platformkit migrate --drain` against the copy while sampling `pg_stat_activity`
for lock waits. It reports one line per file with the duration the runner measured,
then the totals, then a verdict, and exits 0 (applied inside both budgets), 1 (a
migration failed, rule refusals included — the candidate's own message is printed as
soon as it stops, before any query of the step's own can fail over a copy the
candidate never migrated), 2 (it could not run, including a watcher that never
sampled: a number the step did not take is not a pass) or 3 (a budget was overrun or
a file came back contended). A contended file is a finding and never a pass:
discovering it here is the point of doing this before the release rather than during
it.

What it does not measure, and no rehearsal can: the wait behind a table a running
application is reading — there is no application here, and lock waits are sampled
every 100 ms, so a shorter wait can be missed. That is the only gap the step leaves
open on purpose, and it does not report across it: a run whose watcher fell short of
half the samples the window it was alive for resolves to prints `LOCK WATCH BROKEN`
and exits 2, because "0 sample(s) ≈ 0ms" of a run that lasted a minute is a
measurement that never happened — and a floor built from seconds rounded up indicts a
run that was 55 ms long, which is the same fault wearing a red coat. Every report says
how long it watched, so a 0 reads as "nothing waited" or "there was no time to see".
`--max-file-seconds` and `--max-lock-ms` are the operator's budgets, not the kernel's;
`--keep` leaves the two databases behind for comparison, nothing outside those two
names is ever dropped, and a copy whose drop was refused is named on the output as
`LEFT BEHIND` rather than kept quiet.
