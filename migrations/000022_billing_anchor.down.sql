-- Dropping the anchor leaves a subscription whose billing day is read off the
-- period being served, which is the drift this column exists to stop.
ALTER TABLE billing_subscriptions DROP CONSTRAINT IF EXISTS billing_subscriptions_anchor;
ALTER TABLE billing_subscriptions DROP COLUMN IF EXISTS anchor_day;
