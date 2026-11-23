-- Why gather picked each doc, shown to the person by init --gather. Older
-- gathers keep the empty reason.
ALTER TABLE gather_docs ADD COLUMN reason TEXT NOT NULL DEFAULT '';
