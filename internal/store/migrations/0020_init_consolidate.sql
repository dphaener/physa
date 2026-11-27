CREATE TABLE init_consolidate (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    walk_at TEXT NOT NULL,
    at TEXT NOT NULL,
    model TEXT NOT NULL,
    input INTEGER NOT NULL,
    cached_input INTEGER NOT NULL,
    output INTEGER NOT NULL,
    body TEXT NOT NULL
) STRICT;
