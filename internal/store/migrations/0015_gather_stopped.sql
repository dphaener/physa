-- Marks a gather the forge stopped, the date its PRs reached and why. A
-- gather stored before this reads as not stopped.
ALTER TABLE gather_runs ADD COLUMN stopped INTEGER NOT NULL DEFAULT 0;
ALTER TABLE gather_runs ADD COLUMN stopped_reached TEXT NOT NULL DEFAULT '';
ALTER TABLE gather_runs ADD COLUMN stopped_reason TEXT NOT NULL DEFAULT '';
