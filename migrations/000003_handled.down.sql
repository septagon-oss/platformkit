-- Dropping the ledger makes every event in flight eligible to be handled a
-- second time. Stop the workers before rolling this back.
DROP TABLE IF EXISTS platformkit_handled;
