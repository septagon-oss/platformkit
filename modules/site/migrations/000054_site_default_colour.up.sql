-- The colour a site that chose none is rendered in moves to the one the contrast
-- gate would issue for the colour this table was created with.
--
-- site_settings.primary_color was created DEFAULT '#2563eb', and contracts'
-- DefaultPrimaryColor named the same value, so the row, the settings form and the
-- served page agreed with each other and with nothing else: #2563eb reads 4.497:1
-- on the light canvas, three thousandths under the floor the paint measures with,
-- and modules/web measures the accent before it writes it into the head, so no
-- installation ever painted it. Every tenant that never opened the settings screen
-- was rendered in another colour than the one its row held. Moving the column with
-- the constant is what keeps TestTheColumnDefaultIsTheColourTheContractNames — the
-- case that holds the two defaults against each other rather than against a
-- literal — worth running.
--
-- No row is rewritten. A row that holds #2563eb is indistinguishable from a tenant
-- who typed that colour on purpose, and the paint already carries such a row: it
-- measures the stored accent per mode and paints the nearest colour that clears the
-- gate, which is what a reader of that site sees today and will see after this.
ALTER TABLE site_settings
	ALTER COLUMN primary_color SET DEFAULT '#4164b0';
