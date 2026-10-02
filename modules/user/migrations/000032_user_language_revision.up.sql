-- The revision a language choice was read at.
--
-- Migration 000030 refused this column while the choice had one writer and no
-- reader of a revision, and it was right then: a counter nobody compares is a
-- fact nobody reads. Both halves changed with the account door. The choice is now
-- written from two places — the command's own write and the versioned
-- GET/PUT /api/v1/user/me/language that the account page and any client that read
-- the choice first go through — and the versioned door answers a stale write with
-- a conflict rather than with whoever happened to write last. That arbitration is
-- the whole reason a revision exists, so the column is read by the command that
-- writes it, on every write, and returned by the read that hands it out.
--
-- It counts this person's language choices alone, and moves by one per accepted
-- change: a choice that changes nothing moves nothing, which is the same rule the
-- tenant's language counter follows.
ALTER TABLE users
	ADD COLUMN locale_revision bigint NOT NULL DEFAULT 0 CHECK (locale_revision >= 0);
