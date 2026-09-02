-- Dropping the outbox loses every event that has not been relayed yet. Stop the
-- worker and let the queue drain before rolling this back.
DROP TABLE IF EXISTS platformkit_outbox;
