-- Which identity provider each tenant signs in against, and what an address
-- that provider vouches for means here.
--
-- Before this file the installation had one issuer, one client and one client
-- secret for the whole process: `kit/config.OIDC`, handed to `auth.Module` once,
-- with a `*oidc.Provider` behind a mutex behind that. Every request that arrived
-- at any host of any tenant was sent to the same provider, and the only thing
-- per-tenant about single sign-on was the `Host` in the redirect URI. A company
-- whose people live in their own directory and a company whose people live in
-- another one were the same deployment, and one of them was wrong.
--
-- This is decision 0028 applied to the pillar that had not had it: the fact
-- moves to the tenant, the mechanism stays in the module, and the resolution
-- happens per request inside the transaction the `Host` already resolved
-- (`httpx.TenantLoader.ByHost`, 0053 §3). `modules/auth` asks its composition
-- for a provider (`contracts.OIDCProviders`) rather than reaching into this
-- table, so the module still does not know the tenant module exists.
--
-- The secret is not here and cannot be. `oidc_secret_ref` is the *name* of
-- where the secret is — an environment variable, in this repository's own
-- practice (kit/config reads secrets from the environment and nowhere else) —
-- because a row is copied into the outbox payload and from there into
-- `audit_events` (modules/audit/contracts/audit.go), and a secret in a payload
-- is a secret in the trail. An operator's mistake at a keyboard writes a name,
-- and a name reads back as "the provider is not configured"; a mistake that
-- writes a secret would be unreadable in a backup forever.
ALTER TABLE tenants ADD COLUMN oidc_issuer text;
ALTER TABLE tenants ADD COLUMN oidc_client_id text;
ALTER TABLE tenants ADD COLUMN oidc_secret_ref text;
ALTER TABLE tenants ADD COLUMN oidc_redirect_path text;
ALTER TABLE tenants ADD COLUMN oidc_registration text NOT NULL DEFAULT 'existing';
ALTER TABLE tenants ADD COLUMN oidc_roles text[] NOT NULL DEFAULT '{}';

-- `disabled` is a tenant saying "single sign-on is not what we use here", which
-- the module answers with 404 before it dials anything; `existing` — the default,
-- and so the answer every tenant that exists gives today — refuses an address the
-- provider vouches for and this tenant has no account for; `provision` makes that
-- person with the named roles and nothing else.
ALTER TABLE tenants ADD CONSTRAINT tenants_oidc_registration_known CHECK (
	oidc_registration IN ('disabled', 'existing', 'provision')
);

-- Half a provider is unstatable rather than a 500. An issuer with no client id
-- is a redirect to a provider that will refuse the code, and a `provision` with
-- no roles is a door into an empty room: both are writes that cannot be
-- exercised, which is the shape this repository refuses elsewhere (SetRole over
-- an undefined permission, SetLocale over a default outside its own set). The
-- roles rule is here rather than only in Go because the column is readable by a
-- tenant transaction and the pair of them must not be able to disagree.
ALTER TABLE tenants ADD CONSTRAINT tenants_oidc_complete CHECK (
	(oidc_issuer IS NULL AND oidc_client_id IS NULL AND oidc_secret_ref IS NULL
		AND oidc_roles = '{}' AND oidc_registration = 'existing')
	OR
	(oidc_issuer IS NOT NULL AND oidc_client_id IS NOT NULL AND oidc_secret_ref IS NOT NULL
		AND (oidc_registration <> 'provision' OR cardinality(oidc_roles) > 0))
);

-- The mode a tenant is registered in is not a secret: the sign-in page has to
-- say what it will do, and a page that cannot is a page that guesses. So the
-- columns ride the policy 000006 puts over `tenants` — a tenant transaction
-- reads its own row and writes nothing anywhere on this table — and no new
-- policy is created here. `modules/tenant`'s `OIDCOf` is that read; `SetOIDC`
-- and `ClearOIDC` take a `db.Tx[db.System]`, which is what makes an operator's
-- write greppable rather than merely intended (docs/adr/0006).
--
-- data: exempt reason: this revision adds five nullable columns, one column
-- with a default every existing row already satisfies, and two CHECKs both of
-- which every existing row already satisfies; it writes no rows, so no
-- `phase=data` half stands beside it. migrations/README.md says what this line
-- means and where a file that does write rows has to take them.

COMMENT ON COLUMN tenants.oidc_issuer IS
	'platformkit:tenant-scoping-exempt: control-plane column; read per request by auth through contracts.OIDCProviders';
COMMENT ON COLUMN tenants.oidc_secret_ref IS
	'platformkit:secret-reference: the name of a secret, never the secret (modules/auth/contracts.Secrets)';
