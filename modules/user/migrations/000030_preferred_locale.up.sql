-- A person's durable language choice. Empty means the browser and the tenant's
-- own default choose, which is a value rather than an absence: it is how a choice
-- is taken back, and it is the state most people stay in.
--
-- No revision column beside it. The account command has one writer — the person
-- signed in, on their own row, about one field — so the concurrency an expected
-- revision exists to arbitrate is one person's two tabs choosing twice, and the
-- losing tab is told which language they now have every time the page answers.
-- A column no command consults is a fact nobody reads (house rule 7), which is
-- what review round 1 refused here; an integer that is written but never compared
-- would be the same refusal a second time. If this preference ever grows a second
-- writer, it grows the column and the expected argument in the same commit as the
-- writer that needs them.
ALTER TABLE users
	ADD COLUMN preferred_locale text NOT NULL DEFAULT '';
