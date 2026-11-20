CREATE TABLE pr_checks (
    pr INTEGER NOT NULL,
    head_sha TEXT NOT NULL,
    check_run_id INTEGER NOT NULL,
    completed_at TEXT NOT NULL,
    result TEXT NOT NULL,
    PRIMARY KEY (pr, head_sha)
) STRICT;
