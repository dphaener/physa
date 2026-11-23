-- Bets become pursuits, and a pursuit records its signal: the claim, what
-- counts for it and against it, the window it is read in (YYYY-MM-DD) and its
-- resolver, a named person or check. A pursuit started before signals keeps
-- them all NULL. Its appetite is optional: NULL means none, so no circuit
-- breaker, while spend is still counted. Pitches are no longer written or
-- read; their table and its rows stay as they were, as does each pursuit's
-- pitch_id, so nothing is lost. Like 0008's, these tables are written only by
-- the engine (internal/state, 03 section 5.1). Old events keep the kinds and
-- data they were recorded with.

-- Renaming the table rewrites the foreign keys that reference it.
ALTER TABLE bets RENAME TO pursuits;
ALTER TABLE plans RENAME COLUMN bet_id TO pursuit_id;
ALTER TABLE changes RENAME COLUMN bet_id TO pursuit_id;
ALTER TABLE spends RENAME COLUMN bet_id TO pursuit_id;
ALTER TABLE needs_you RENAME COLUMN bet_id TO pursuit_id;
ALTER TABLE evidence RENAME COLUMN bet TO pursuit;
DROP INDEX evidence_bet;
CREATE INDEX evidence_pursuit ON evidence(pursuit, at);

-- SQLite cannot drop NOT NULL in place, so the appetite moves to a new
-- column that allows NULL, under its old name.
ALTER TABLE pursuits ADD COLUMN appetite INTEGER CHECK (appetite > 0);
UPDATE pursuits SET appetite = appetite_tokens;
ALTER TABLE pursuits DROP COLUMN appetite_tokens;
ALTER TABLE pursuits RENAME COLUMN appetite TO appetite_tokens;

ALTER TABLE pursuits ADD COLUMN signal_claim TEXT;
ALTER TABLE pursuits ADD COLUMN signal_for TEXT;
ALTER TABLE pursuits ADD COLUMN signal_against TEXT;
ALTER TABLE pursuits ADD COLUMN signal_window TEXT;
ALTER TABLE pursuits ADD COLUMN signal_resolver TEXT;
