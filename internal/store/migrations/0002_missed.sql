-- The reader said they should have got this story and did not.
ALTER TABLE stories ADD COLUMN missed_at INTEGER;
ALTER TABLE stories ADD COLUMN missed_note TEXT;
