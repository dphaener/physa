-- The engine's tables. They are written only by the engine (internal/state,
-- 03 section 5.1), and events are never replayed to answer a read: the state
-- tables always hold the current answer.

CREATE TABLE ideas (
    id         INTEGER PRIMARY KEY,
    slug       TEXT NOT NULL UNIQUE,
    text       TEXT NOT NULL,
    person     TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

-- No UNIQUE on idea_id: a bet stopped by its circuit breaker goes back to
-- shaping, and reshaping adds a new pitch while the stopped bet keeps
-- pointing at its own.
CREATE TABLE pitches (
    id              INTEGER PRIMARY KEY,
    idea_id         INTEGER NOT NULL REFERENCES ideas(id),
    problem         TEXT NOT NULL DEFAULT '',
    appetite_tokens INTEGER NOT NULL CHECK (appetite_tokens > 0),
    solution        TEXT NOT NULL DEFAULT '',
    scope           TEXT NOT NULL DEFAULT '',
    signal          TEXT NOT NULL DEFAULT '',
    rabbit_holes    TEXT NOT NULL DEFAULT '',
    no_gos          TEXT NOT NULL DEFAULT '',
    person          TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
) STRICT;
CREATE INDEX pitches_idea ON pitches(idea_id);

CREATE TABLE bets (
    id              INTEGER PRIMARY KEY,
    slug            TEXT NOT NULL UNIQUE,
    idea_id         INTEGER NOT NULL REFERENCES ideas(id),
    pitch_id        INTEGER REFERENCES pitches(id),
    person          TEXT NOT NULL,
    appetite_tokens INTEGER NOT NULL CHECK (appetite_tokens > 0),
    spent_tokens    INTEGER NOT NULL DEFAULT 0,
    state           TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'done', 'stopped')),
    stop_reason     TEXT,
    created_at      TEXT NOT NULL,
    started_at      TEXT,
    ended_at        TEXT
) STRICT;

CREATE TABLE plans (
    id         INTEGER PRIMARY KEY,
    bet_id     INTEGER NOT NULL UNIQUE REFERENCES bets(id),
    created_at TEXT NOT NULL
) STRICT;

-- changes is a JSON array of planned changes.
CREATE TABLE plan_revisions (
    id         INTEGER PRIMARY KEY,
    plan_id    INTEGER NOT NULL REFERENCES plans(id),
    n          INTEGER NOT NULL,
    changes    TEXT NOT NULL,
    reason     TEXT NOT NULL,
    person     TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (plan_id, n)
) STRICT;

CREATE TABLE events (
    id         INTEGER PRIMARY KEY,
    at         TEXT NOT NULL,
    kind       TEXT NOT NULL,
    subject    TEXT NOT NULL,
    subject_id INTEGER,
    person     TEXT,
    data       TEXT NOT NULL DEFAULT '{}'
) STRICT;
CREATE INDEX events_at ON events(at);

CREATE TABLE intents (
    id         INTEGER PRIMARY KEY,
    kind       TEXT NOT NULL,
    payload    TEXT NOT NULL DEFAULT '{}',
    state      TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'done', 'failed')),
    attempts   INTEGER NOT NULL DEFAULT 0,
    next_at    TEXT NOT NULL,
    last_error TEXT,
    result     TEXT,
    created_at TEXT NOT NULL,
    done_at    TEXT
) STRICT;
CREATE INDEX intents_due ON intents(state, next_at);

CREATE TABLE timers (
    id      INTEGER PRIMARY KEY,
    kind    TEXT NOT NULL,
    key     TEXT NOT NULL,
    due_at  TEXT NOT NULL,
    payload TEXT NOT NULL DEFAULT '{}',
    UNIQUE (kind, key)
) STRICT;
CREATE INDEX timers_due ON timers(due_at);
