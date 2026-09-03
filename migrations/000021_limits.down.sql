-- Dropping the counters leaves an application whose limits are per process
-- again, which is where they were before this table.
DROP TABLE IF EXISTS platformkit_limits;
