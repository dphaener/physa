-- Who merged each gathered PR, so recovery can credit a choice an agent wrote
-- to the person who accepted it. Empty when the forge did not say.
ALTER TABLE gather_prs ADD COLUMN merged_by TEXT NOT NULL DEFAULT '';
