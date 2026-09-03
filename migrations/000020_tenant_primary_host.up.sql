-- Which of a tenant's hosts is the one to name.
--
-- A tenant may answer at several, and something has to pick one whenever an
-- absolute URL is built: a mailed set-password link, a notice's link, anything
-- a person clicks from outside the application. Until now the answer was "the
-- first one alphabetically", which is what ORDER BY host means and is not an
-- answer at all — adding admin.acme.example.com silently moved every future
-- link off acme.example.com, and nobody chose that.
--
-- The column is is_primary and not primary, because PRIMARY is a reserved word
-- in Postgres and a column that has to be quoted in every statement is a column
-- somebody will one day forget to quote.
ALTER TABLE tenant_hosts ADD COLUMN is_primary boolean NOT NULL DEFAULT false;

-- The rows that already exist get the answer they already had, so nothing moves
-- on the deploy that runs this: the alphabetically first host of each tenant is
-- the one every link has been built on.
UPDATE tenant_hosts SET is_primary = true
WHERE host IN (SELECT DISTINCT ON (tenant_id) host FROM tenant_hosts ORDER BY tenant_id, host);

-- At most one per tenant, enforced rather than remembered: two primaries is a
-- link that depends on which row the planner returned first.
CREATE UNIQUE INDEX tenant_hosts_primary ON tenant_hosts (tenant_id) WHERE is_primary;
