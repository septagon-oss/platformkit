-- The rich-text field measures canonical Markdown code points. Four UTF-8
-- bytes per code point is a bound on renderer input without excluding valid text.
ALTER TABLE contents DROP CONSTRAINT contents_body;
ALTER TABLE contents ADD CONSTRAINT contents_body
  CHECK (char_length(body) <= 262144 AND octet_length(body) <= 1048576);
