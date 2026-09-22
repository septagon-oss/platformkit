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
