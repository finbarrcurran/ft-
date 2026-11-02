-- Jordi-46 Phase 1 — crypto_index_members: the constituents of the Visser-Labs 46-name tokenized index
-- (list_version 2026-09-21), as a reference table only. No scoring, no alerts, no price storage. It is
-- invisible to watchlist, stock_holdings, crypto_holdings, theses, alerts and entry-zone logic: join by
-- SYMBOL if ever needed (crypto_theses.coingecko_id is NULL on 7 of 18 rows). Append-only convention:
-- a member leaving the index gets removed_at, the row is never deleted.
CREATE TABLE IF NOT EXISTS crypto_index_members (
  id             INTEGER PRIMARY KEY,
  index_key      TEXT    NOT NULL,                       -- 'visser46'
  list_version   TEXT    NOT NULL,                       -- date of the constituent list, '2026-09-21'
  symbol         TEXT    NOT NULL,
  name           TEXT    NOT NULL,
  vertical       TEXT    NOT NULL,
  asset_kind     TEXT    NOT NULL CHECK (asset_kind IN ('token','stock')),
  coingecko_id   TEXT,                                   -- tokens
  stock_ticker   TEXT,                                   -- stocks
  exchange       TEXT,                                   -- stocks, when verified
  added_at       INTEGER NOT NULL DEFAULT (strftime('%s','now')),
  removed_at     INTEGER,                                -- soft removal
  source_note    TEXT,
  UNIQUE (index_key, list_version, symbol)
);
CREATE INDEX IF NOT EXISTS idx_cim_index ON crypto_index_members(index_key, removed_at);

INSERT OR IGNORE INTO crypto_index_members
  (index_key, list_version, symbol, name, vertical, asset_kind, coingecko_id, stock_ticker, exchange, source_note) VALUES
  ('visser46','2026-09-21','TAO','Bittensor','AI Agents & Machine Economy','token','bittensor',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','VIRTUAL','Virtuals Protocol','AI Agents & Machine Economy','token','virtual-protocol',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','VVV','Venice Token','AI Agents & Machine Economy','token','venice-token',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','AKT','Akash','DePIN, Storage & Physical Networks','token','akash-network',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','AR','Arweave','DePIN, Storage & Physical Networks','token','arweave',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','FIL','Filecoin','DePIN, Storage & Physical Networks','token','filecoin',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','GEOD','GEODNET','DePIN, Storage & Physical Networks','token','geodnet',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','GRASS','Grass','DePIN, Storage & Physical Networks','token','grass',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','HNT','Helium','DePIN, Storage & Physical Networks','token','helium',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','IOTX','IoTeX','DePIN, Storage & Physical Networks','token','iotex',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','RENDER','Render','DePIN, Storage & Physical Networks','token','render-token',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','BTC','Bitcoin','Digital Monetary Assets','token','bitcoin',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','XMR','Monero','Digital Monetary Assets','token','monero',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','ZEC','Zcash','Digital Monetary Assets','token','zcash',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','COIN','Coinbase','Exchange, Brokerage & Market Structure','stock',NULL,'COIN',NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','HOOD','Robinhood','Exchange, Brokerage & Market Structure','stock',NULL,'HOOD',NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','HYPE','Hyperliquid','Exchange, Brokerage & Market Structure','token','hyperliquid',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','JUP','Jupiter','Exchange, Brokerage & Market Structure','token','jupiter-exchange-solana',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','RUNE','THORChain','Exchange, Brokerage & Market Structure','token','thorchain',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','UNI','Uniswap','Exchange, Brokerage & Market Structure','token','uniswap',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','AAVE','Aave','Lending, Credit & Rates','token','aave',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','MORPHO','Morpho','Lending, Credit & Rates','token','morpho',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','PENDLE','Pendle','Lending, Credit & Rates','token','pendle',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','SYRUP','Maple Finance','Lending, Credit & Rates','token','syrup',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','GRT','The Graph','Oracle, Data & Interop Middleware','token','the-graph',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','LINK','Chainlink','Oracle, Data & Interop Middleware','token','chainlink',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','PYTH','Pyth Network','Oracle, Data & Interop Middleware','token','pyth-network',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','ZRO','LayerZero','Oracle, Data & Interop Middleware','token','layerzero',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','CC','Canton','Settlement & Smart Contract Rails','token','canton-network',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only; thin volume (about $8M a day on 2026-11-01): prices may be thin'),
  ('visser46','2026-09-21','ETH','Ethereum','Settlement & Smart Contract Rails','token','ethereum',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','NEAR','NEAR Protocol','Settlement & Smart Contract Rails','token','near',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','SOL','Solana','Settlement & Smart Contract Rails','token','solana',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','SUI','Sui','Settlement & Smart Contract Rails','token','sui',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','TRX','TRON','Settlement & Smart Contract Rails','token','tron',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','XLM','Stellar','Settlement & Smart Contract Rails','token','stellar',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','XRP','XRP','Settlement & Smart Contract Rails','token','ripple',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','CRCL','Circle','Stablecoin & Synthetic-Dollar Systems','stock',NULL,'CRCL',NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','ENA','Ethena','Stablecoin & Synthetic-Dollar Systems','token','ethena',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','SKY','Sky (Maker)','Stablecoin & Synthetic-Dollar Systems','token','sky',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','ETHFI','Ether.fi','Staking, Restaking & Validation','token','ether-fi',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','JTO','Jito','Staking, Restaking & Validation','token','jito-governance-token',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','LDO','Lido DAO','Staking, Restaking & Validation','token','lido-dao',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','CFG','Centrifuge','Tokenization & RWA Rails','token','centrifuge-2',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','FIGR','Figure Technology','Tokenization & RWA Rails','stock',NULL,'FIGR','NASDAQ','Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only'),
  ('visser46','2026-09-21','HASH','Provenance Blockchain','Tokenization & RWA Rails','token','hash-2',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only; thin volume (about $66K a day on 2026-11-01): prices may be stale'),
  ('visser46','2026-09-21','ONDO','Ondo','Tokenization & RWA Rails','token','ondo-finance',NULL,NULL,'Visser-Labs 46-name index, close 2026-09-21; constituent list and vertical labels only');
