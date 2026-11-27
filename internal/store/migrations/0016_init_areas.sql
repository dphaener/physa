CREATE TABLE init_areas (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    gather_at TEXT NOT NULL,
    at TEXT NOT NULL,
    model TEXT NOT NULL,
    input INTEGER NOT NULL,
    cached_input INTEGER NOT NULL,
    output INTEGER NOT NULL,
    body TEXT NOT NULL
) STRICT;
