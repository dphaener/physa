CREATE TABLE slice_records (
    id INTEGER PRIMARY KEY,
    at TEXT NOT NULL,
    capabilities TEXT NOT NULL,
    "with" TEXT NOT NULL,
    budget INTEGER,
    session TEXT
) STRICT;
CREATE INDEX slice_records_at ON slice_records (at);
