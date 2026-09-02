-- Dropping this loses the record of what never got handled. Nothing redelivers
-- from it, so nothing is queued; what is lost is the evidence.
DROP TABLE IF EXISTS platformkit_dead_letters;
