-- v1.10.5 — Signals: enrich rows with issuer name.
--
-- The Form 4 XML <ownershipDocument><issuer><issuerName> is the
-- company's full legal name (e.g. "CAMPBELL'S Co" for ticker CPB).
-- Capture it on the row so the UI doesn't have to look it up.
--
-- Sector is still computed at read-time via a JOIN to holdings /
-- watchlist / sector_universe so it stays fresh when the user adds
-- or removes positions.

ALTER TABLE signal_events ADD COLUMN issuer_name TEXT NULL;
