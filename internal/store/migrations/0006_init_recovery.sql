CREATE TABLE init_recovery (
    capability TEXT PRIMARY KEY,
    proposal_at TEXT NOT NULL,
    at TEXT NOT NULL,
    status TEXT NOT NULL,
    file TEXT NOT NULL,
    terms TEXT NOT NULL,
    details TEXT NOT NULL,
    session_id TEXT NOT NULL,
    model TEXT NOT NULL,
    attempts INTEGER NOT NULL,
    appetite INTEGER NOT NULL,
    input INTEGER NOT NULL,
    cached_input INTEGER NOT NULL,
    output INTEGER NOT NULL
) STRICT;
