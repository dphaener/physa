-- Engineering evidence: facts observed in version control and on the forge,
-- each recorded once. Like 0008's and 0009's, these tables are written only
-- by the engine (internal/state, 03 section 5.1). A fact holds no weight or
-- strength: strength is worked out from the facts, never stored with them.

-- kind names the fact (a change landing, a revert, a lead time, a CI result);
-- change is the plan change's name from its Change: trailer; seconds is set
-- for a lead time only; by_commit is the reverting change of a revert; source
-- is where it was observed.
CREATE TABLE evidence (
    id          INTEGER PRIMARY KEY,
    kind        TEXT NOT NULL,
    commit_id   TEXT NOT NULL DEFAULT '',
    pr          INTEGER NOT NULL DEFAULT 0,
    capability  TEXT NOT NULL DEFAULT '',
    bet         TEXT NOT NULL DEFAULT '',
    change      TEXT NOT NULL DEFAULT '',
    idea        TEXT NOT NULL DEFAULT '',
    at          TEXT NOT NULL,
    source      TEXT NOT NULL CHECK (source IN ('vcs', 'forge')),
    seconds     INTEGER,
    by_commit   TEXT NOT NULL DEFAULT '',
    recorded_at TEXT NOT NULL,
    UNIQUE (kind, commit_id, pr)
) STRICT;
CREATE INDEX evidence_capability ON evidence(capability, at);
CREATE INDEX evidence_bet ON evidence(bet, at);
CREATE INDEX evidence_at ON evidence(at);

-- One row per store: how far version control and the forge have been read,
-- and the forge's backoff after failures.
CREATE TABLE evidence_polls (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    vcs_commit      TEXT NOT NULL DEFAULT '',
    vcs_at          TEXT NOT NULL DEFAULT '',
    forge_merged_at TEXT NOT NULL DEFAULT '',
    forge_failures  INTEGER NOT NULL DEFAULT 0,
    forge_next_at   TEXT NOT NULL DEFAULT ''
) STRICT;
