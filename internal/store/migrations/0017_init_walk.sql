CREATE TABLE init_walk (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    areas_at TEXT NOT NULL,
    at TEXT NOT NULL,
    model TEXT NOT NULL,
    input INTEGER NOT NULL,
    cached_input INTEGER NOT NULL,
    output INTEGER NOT NULL,
    body TEXT NOT NULL
) STRICT;
