-- What each init phase spent, as the run's meter counted it from the
-- adapter's real dollars: its raw tokens and its weighted tokens with their
-- cost estimate in millionths of a dollar, so a later write shows what was
-- really spent. One row per phase; a new gather clears them all, a new
-- proposal clears propose's and recover's.
CREATE TABLE init_spend (
    phase TEXT PRIMARY KEY,
    input INTEGER NOT NULL,
    cached_input INTEGER NOT NULL,
    output INTEGER NOT NULL,
    weighted INTEGER NOT NULL,
    micro_usd INTEGER NOT NULL,
    unpriced INTEGER NOT NULL
) STRICT;
