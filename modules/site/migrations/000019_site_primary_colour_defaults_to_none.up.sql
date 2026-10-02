-- The colour a site that chose none holds is none, not a colour picked for it.
--
-- modules/site/contracts/site.go's DefaultPrimaryColor is the one constant the
-- read, the settings form and the paint answer from, and it now names no colour.
-- A #rrggbb could not do the job: the gate measures the accent as body copy on
-- each mode's own canvas, so one stored value would have to read at 4.5:1 on a
-- light canvas and on a dark one, which needs those grounds 4.5 x 4.5 = 20.25:1
-- apart, and design.Default() puts them 18.28:1 apart. What this column used to
-- default to, #2563eb, reads on neither (4.50:1 on the light canvas against a
-- floor of 4.5), so the colour a tenant that never opened the form was rendered
-- in was never the colour its row held. See CHANGELOG.md.
--
-- Only the default moves. No UPDATE runs here: a row that holds #2563eb may hold
-- the colour somebody typed, and a migration cannot tell that person's choice
-- from the old default, so it makes no claim about either. A tenant whose row
-- already carries a colour keeps it, measured at the paint; a tenant that has
-- never saved holds none and wears the accent of the palette its site links.

ALTER TABLE site_settings ALTER COLUMN primary_color SET DEFAULT '';
