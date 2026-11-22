-- A bet's planned changes, the session working on each, tokens spent, and
-- the Needs you items the appetite raises. Like 0008's, these tables are
-- written only by the engine (internal/state, 03 section 5.1).

-- planned is 0 once a later revision drops the change; the row stays so its
-- spend stays.
CREATE TABLE changes (
    id            INTEGER PRIMARY KEY,
    bet_id        INTEGER NOT NULL REFERENCES bets(id),
    name          TEXT NOT NULL,
    capability    TEXT NOT NULL,
    planned       INTEGER NOT NULL DEFAULT 1 CHECK (planned IN (0, 1)),
    state         TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'landed')),
    landed_commit TEXT,
    landed_at     TEXT,
    spent_tokens  INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE (bet_id, name)
) STRICT;

CREATE TABLE session_changes (
    id         INTEGER PRIMARY KEY,
    session    TEXT NOT NULL UNIQUE,
    change_id  INTEGER NOT NULL REFERENCES changes(id),
    at         TEXT NOT NULL
) STRICT;

-- change is the change's name, '' for the bet itself; tokens is the
-- session's running total there.
CREATE TABLE spends (
    id      INTEGER PRIMARY KEY,
    bet_id  INTEGER NOT NULL REFERENCES bets(id),
    change  TEXT NOT NULL DEFAULT '',
    session TEXT NOT NULL,
    tokens  INTEGER NOT NULL CHECK (tokens >= 0),
    at      TEXT NOT NULL,
    UNIQUE (bet_id, change, session)
) STRICT;

CREATE TABLE needs_you (
    id         INTEGER PRIMARY KEY,
    person     TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('appetite', 'breaker')),
    bet_id     INTEGER REFERENCES bets(id),
    text       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    done_at    TEXT
) STRICT;
CREATE INDEX needs_you_person ON needs_you(person, done_at);
