-- Init no longer proposes a map: code first names the capabilities from the
-- walk of the repo's code, so the stored proposal goes. A record that holds
-- only a proposal and its recoveries can no longer be written as a map; a
-- new physa init replaces it. The recoveries and init's spend are kept.
DROP TABLE IF EXISTS init_proposal;
