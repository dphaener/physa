CREATE TABLE gather_runs (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    at TEXT NOT NULL,
    since TEXT NOT NULL,
    max_prs INTEGER NOT NULL,
    prs_limited INTEGER NOT NULL,
    forge TEXT NOT NULL,
    forge_reason TEXT NOT NULL
) STRICT;
CREATE TABLE gather_docs (
    path TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    body TEXT NOT NULL
) STRICT;
CREATE TABLE gather_changes (
    id TEXT PRIMARY KEY,
    short TEXT NOT NULL,
    subject TEXT NOT NULL,
    message TEXT NOT NULL,
    author TEXT NOT NULL,
    at TEXT NOT NULL,
    trailers TEXT NOT NULL,
    reverts TEXT NOT NULL,
    files TEXT NOT NULL,
    seq INTEGER NOT NULL
) STRICT;
CREATE TABLE gather_prs (
    number INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    author TEXT NOT NULL,
    merged_at TEXT NOT NULL,
    issues TEXT NOT NULL,
    seq INTEGER NOT NULL
) STRICT;
CREATE TABLE gather_comments (
    pr INTEGER NOT NULL,
    author TEXT NOT NULL,
    at TEXT NOT NULL,
    kind TEXT NOT NULL,
    requests_changes INTEGER NOT NULL,
    body TEXT NOT NULL,
    seq INTEGER NOT NULL
) STRICT;
CREATE INDEX gather_comments_pr ON gather_comments (pr, seq);
