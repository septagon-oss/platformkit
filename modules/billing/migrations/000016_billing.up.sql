-- Billing: the plans a tenant can buy, and the one subscription it has.
--
-- The entities are modules/billing/contracts/billing.go and the columns below
-- are their fields; the module owns the SQL, the ledger is shared, and the file
-- lives here because there is one migration directory (ARCHITECTURE.md, idea 7).

CREATE TABLE billing_plans (
	id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id   uuid NOT NULL,
	created_at  timestamptz NOT NULL DEFAULT now(),
	updated_at  timestamptz NOT NULL DEFAULT now(),
	deleted_at  timestamptz,

	-- What an invoice, a price page and an integration call this plan.
	code        varchar(60) NOT NULL,
	name        varchar(120) NOT NULL,
	-- The price of one period, in the minor unit of the currency. Money in a
	-- float is wrong and money in a decimal is a dependency; money in cents is
	-- an integer every language and every database agrees about.
	price_cents bigint NOT NULL DEFAULT 0,
	currency    char(3) NOT NULL,
	-- month or year. There is no day and no week: a subscription business bills
	-- monthly or yearly, and an interval nobody sells is a branch every renewal
	-- would have to carry.
	interval    varchar(10) NOT NULL DEFAULT 'month',
	-- Feature names, not permissions: what a name entitles somebody to is the
	-- consuming module's business, so a plan can be re-priced without touching
	-- an authorization.
	features    text[] NOT NULL DEFAULT '{}',
	-- Whether the plan accepts new subscriptions. It gates enrollment and not
	-- existence: the subscriptions already on a deactivated plan keep running.
	-- The API requires it rather than defaulting it, because a Go bool cannot
	-- tell false from absent and both GORM and huma guess; the entity says so.
	active      boolean NOT NULL DEFAULT false
);

-- Unique across the installation and not per tenant, because there is one
-- catalogue: two rows with the same code would be two prices for one plan, and
-- which one a tenant saw would depend on the sort. Partial, so a deleted plan
-- releases its code.
CREATE UNIQUE INDEX billing_plans_code ON billing_plans (code) WHERE deleted_at IS NULL;
-- The default list page. It is not prefixed by the tenant either: every tenant
-- reads the same rows.
CREATE INDEX billing_plans_created ON billing_plans (created_at DESC, id)
	WHERE deleted_at IS NULL;

CREATE TABLE billing_subscriptions (
	id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id            uuid NOT NULL,
	created_at           timestamptz NOT NULL DEFAULT now(),
	updated_at           timestamptz NOT NULL DEFAULT now(),
	deleted_at           timestamptz,

	-- The plan being billed. There is no foreign key and there does not need to
	-- be one: the delete route refuses a plan somebody is still subscribed to,
	-- which is a rule about live subscriptions that a foreign key could not
	-- express anyway.
	plan_id              uuid NOT NULL,
	-- trial, active, past_due or cancelled.
	status               varchar(20) NOT NULL DEFAULT 'trial',
	-- The period being served. The next one starts exactly where this one ends,
	-- so periods chain rather than drift with the hour a job happened to run.
	current_period_start timestamptz NOT NULL,
	current_period_end   timestamptz NOT NULL,
	-- A customer who has left and is still owed what they paid for. Nothing
	-- shortens the period: cancelling is not a refund.
	cancel_at_period_end boolean NOT NULL DEFAULT false,

	-- The plan's price as it was when this period started, stamped here rather
	-- than read from the plan at renewal: without it a price list edit was
	-- retroactive, and changing a plan's currency changed what a live
	-- subscriber was billed in. Re-pricing applies from the next period.
	price_cents          bigint NOT NULL DEFAULT 0,
	currency             varchar(3) NOT NULL DEFAULT '',
	-- When this tenant's one trial was issued, and never cleared. Cancelling
	-- and resubscribing used to reissue it, so four cancellations were four
	-- free periods.
	trial_used_at        timestamptz,
	-- The dunning state: consecutive failures, and when the first one was. Past
	-- the module's grace period the subscription is cancelled, which is the
	-- ceiling a dead card needs — without it a renewal retried it every night
	-- and served the customer for as long as it did.
	attempt_count        integer NOT NULL DEFAULT 0,
	past_due_since       timestamptz,

	-- A period ends after it starts. The entity's Validate says the same thing,
	-- and this is the half that is true of a row no Go code wrote.
	CONSTRAINT billing_subscriptions_period CHECK (current_period_end > current_period_start)
);

-- One subscription per tenant: a tenant is the customer, so it has one. This is
-- the whole reason the routes are a singleton and not a collection, and it is
-- also the only index the table needs — a lookup by tenant is this index and
-- there is nothing else to ask.
CREATE UNIQUE INDEX billing_subscriptions_tenant ON billing_subscriptions (tenant_id)
	WHERE deleted_at IS NULL;

-- The shape every tenant-owned table has; migrations/000001 explains it. FORCE
-- is what binds the owner, and without it the application role escapes the
-- policy on any table it created itself.
--
-- billing_plans is the exception, and it is the one thing in this file worth
-- reading twice. The price list is the operator's, not the tenant's: it was a
-- tenant-scoped, tenant-writable table under the same permission that enrolls,
-- so a customer created a plan of its own at a price of its own and moved to
-- it — which a review did, from past_due, and its debt went with the old price.
--
-- So the read side is open and the write side is the operator's. USING (true)
-- because every tenant has to see the catalogue it is choosing from, and the
-- rows are one shared list rather than a copy per tenant, so there is nothing
-- to leak: a plan is a public price. WITH CHECK keeps the tenant match, which
-- means a row can only be written by a transaction scoped to the tenant it
-- names — and the only transactions that reach the write routes are the
-- operator's, because those routes declare httpx.OperatorPermission and the
-- kernel refuses them at any other tenant before it asks the roles table
-- anything. See modules/billing/module.go and docs/adr/0008.
ALTER TABLE billing_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_plans FORCE ROW LEVEL SECURITY;
CREATE POLICY billing_plans_catalogue ON billing_plans
	USING (true)
	WITH CHECK (platformkit_tenant_match(tenant_id));
COMMENT ON TABLE billing_plans IS 'platformkit:tenant-scoping-exempt: one catalogue, read by every tenant, written only at the operator''s host under billing:catalog (docs/adr/0008)';

ALTER TABLE billing_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_subscriptions FORCE ROW LEVEL SECURITY;
CREATE POLICY billing_subscriptions_tenant ON billing_subscriptions
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
