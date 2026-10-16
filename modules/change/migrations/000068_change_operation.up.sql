-- The server copies this link from admission; historical proposals stay unbound.
ALTER TABLE change_proposals ADD COLUMN operation_ref uuid;
