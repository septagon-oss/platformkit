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

-- Unique per tenant, and partial so a deleted plan releases its code.
CREATE UNIQUE INDEX billing_plans_tenant_code ON billing_plans (tenant_id, code)
	WHERE deleted_at IS NULL;
-- The default list page.
CREATE INDEX billing_plans_tenant_created ON billing_plans (tenant_id, created_at DESC, id)
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
ALTER TABLE billing_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_plans FORCE ROW LEVEL SECURITY;
CREATE POLICY billing_plans_tenant ON billing_plans
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

ALTER TABLE billing_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_subscriptions FORCE ROW LEVEL SECURITY;
CREATE POLICY billing_subscriptions_tenant ON billing_subscriptions
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
