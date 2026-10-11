-- Migration 0050 — SC-41b: OAuth 2.1 authorization server for the claude.ai MCP connector.
--
-- Single-user, read-only. Public clients with PKCE (S256); clients register
-- through RFC 7591 (/register) or are created by hand (`ft oauth create-client`).
-- Codes and tokens are stored as sha256 hashes only. Refresh tokens rotate; a
-- reused (already rotated) refresh token revokes its whole family.
-- No existing table is altered.

CREATE TABLE IF NOT EXISTS oauth_clients (
  client_id         TEXT PRIMARY KEY,
  client_name       TEXT,
  redirect_uris     TEXT NOT NULL,            -- JSON array, matched exactly
  secret_hash       TEXT,                     -- NULL = public client (the default)
  source            TEXT NOT NULL,            -- 'dcr' | 'manual'
  created_at        INTEGER NOT NULL,
  last_used_at      INTEGER
);

CREATE TABLE IF NOT EXISTS oauth_codes (
  code_hash         TEXT PRIMARY KEY,
  client_id         TEXT NOT NULL REFERENCES oauth_clients(client_id) ON DELETE CASCADE,
  user_id           INTEGER NOT NULL,
  redirect_uri      TEXT NOT NULL,
  code_challenge    TEXT NOT NULL,
  resource          TEXT NOT NULL,
  scope             TEXT NOT NULL,
  expires_at        INTEGER NOT NULL,
  used_at           INTEGER
);

CREATE TABLE IF NOT EXISTS oauth_tokens (
  id                  INTEGER PRIMARY KEY,
  family_id           TEXT NOT NULL,          -- all tokens descended from one authorization
  client_id           TEXT NOT NULL REFERENCES oauth_clients(client_id) ON DELETE CASCADE,
  user_id             INTEGER NOT NULL,
  scope               TEXT NOT NULL,
  resource            TEXT NOT NULL,
  access_hash         TEXT NOT NULL UNIQUE,
  refresh_hash        TEXT NOT NULL UNIQUE,
  access_expires_at   INTEGER NOT NULL,
  refresh_expires_at  INTEGER NOT NULL,
  created_at          INTEGER NOT NULL,
  last_used_at        INTEGER,
  rotated_at          INTEGER,                -- refresh token spent (a newer pair exists)
  revoked_at          INTEGER
);
CREATE INDEX IF NOT EXISTS idx_oauth_tokens_family ON oauth_tokens(family_id);
