CREATE TABLE init_place (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    walk_at TEXT NOT NULL,
    at TEXT NOT NULL,
    body TEXT NOT NULL
) STRICT;
