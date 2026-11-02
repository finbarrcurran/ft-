-- Jordi-46 Phase 1 follow-up — record the exchange of the three index stocks seeded without one. Checked
-- 2026-11-02 against both providers' own responses (Finnhub profile2 "exchange" and Yahoo chart meta
-- "fullExchangeName"), not from memory: COIN and HOOD Nasdaq (Nasdaq Global Select / NMS), CRCL New York Stock Exchange.
UPDATE crypto_index_members SET exchange = 'NASDAQ' WHERE index_key = 'visser46' AND symbol IN ('COIN','HOOD') AND exchange IS NULL;
UPDATE crypto_index_members SET exchange = 'NYSE'   WHERE index_key = 'visser46' AND symbol = 'CRCL' AND exchange IS NULL;
