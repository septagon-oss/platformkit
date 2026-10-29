-- The owed-effect count: how many effects the delivery this claim records still owes
-- from the ones it deferred to after its own commit.
--
-- platformkit_handled has one job — make at-least-once delivery run a handler once —
-- and the vocabulary it needed until now was "claimed". A handler that defers an
-- effect past the commit that makes its rows spendable (kit/db's AfterCommit, because
-- the effect cannot be a row: a one-time link written into the outbox is a live
-- credential in a table nobody treats as a credential store) ends its transaction with
-- the claim in and the effect not yet carried out. The claim then says "done" about a
-- delivery that is not: a redelivery of the same event finds the mark taken, skips the
-- handler and acknowledges, the outbox row is stamped published, and the effect never
-- runs again.
--
-- So the claim keeps the fact the acknowledgement decision needs, counted. A handler
-- may defer more than one effect — kit/db runs them as one queue, registered by
-- different work — and one effect coming back says nothing about the others, so a
-- single yes/no that the first success could set would clear a debt the delivery still
-- owes. The column counts what is outstanding. It is a column of this row and not a
-- table: the fact is about a delivery, the row already is that delivery's mark, and it
-- is written and read by the one package that decides the ack. kit/events adds one
-- each time a handler defers an effect (Defer, in the delivery's own transaction, so
-- the count and the claim appear together), takes one off when that effect succeeds,
-- and deletes the row when one fails. Every later delivery reads it FOR UPDATE, so
-- nothing acknowledges from a value the owning attempt is about to change. Zero is the
-- ordinary case: a claim whose handler owed nothing.

ALTER TABLE platformkit_handled
	ADD COLUMN effects_owed integer NOT NULL DEFAULT 0;
