CREATE TABLE init_proposal (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    gather_at TEXT NOT NULL,
    at TEXT NOT NULL,
    session_id TEXT NOT NULL,
    model TEXT NOT NULL,
    appetite INTEGER NOT NULL,
    input INTEGER NOT NULL,
    cached_input INTEGER NOT NULL,
    output INTEGER NOT NULL,
    body TEXT NOT NULL
) STRICT;
