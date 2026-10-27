-- Migration 0060 — SC-52: the newest Jordi digest (the one behind the login card) is extracted
-- three times and the majority state is kept. proposal_runs = how many extractions voted,
-- proposal_agree = how many of them gave the kept state. NULL = a single extraction (every digest
-- except the newest Jordi one). A split (agree < runs) forces confidence to low and the card says
-- "2 of 3 runs".
ALTER TABLE video_digests ADD COLUMN proposal_runs  INTEGER;
ALTER TABLE video_digests ADD COLUMN proposal_agree INTEGER;
