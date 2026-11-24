# FT spec archive — change log, part 08

Full, unedited change-log entries of the FT master spec, oldest first (this file only ever grows at the end; existing text is never edited — a correction goes in a new entry). Map of every version and pass to its file: INDEX.md.

>
> **Overhaul:** 2026-11-24 (Jordi-46 Part B, FT-165) — **The 04:00 job is the only writer of price_history for held coins (v1.145.2).** Fin's ruling FT-165. Part B's daily close and backfill went through `InsertPriceHistoryBatch`, which is `ON CONFLICT DO UPDATE` and so overwrote an existing row. Now Part B skips the six held coins (BTC, ETH, LINK, SOL, SUI, XRP, the keys of `market.SymbolToGeckoID`; all six are index members, so the index has 36 non-held tokens) and any index stock with an active row in `stock_holdings`, and for everything else uses the new insert-only `InsertPriceHistoryIfAbsent` (`ON CONFLICT DO NOTHING`), so an existing row is never changed. The backfill dry run lists the skipped held names; a held coin's history comes from the 04:00 job only. Breadth still reads closes up to yesterday; Fin accepts the offset of about 35 minutes on the 36 non-held tokens (CoinGecko's daily point is the 00:00 UTC price, the close lands at 00:35). The backfill plan is now 36 calls, not 42 (at most about 1,440 + 36 a month added). Tested: an existing row is not changed by the close, a held coin and a held stock get no Part B rows, a missing row is still inserted. Migrations: none. Supersedes v1.145.1.

<!-- END FT-spec-archive changelog-08 -->
