-- Вход вне Telegram: сессии, коды из бота, пароли, passkey (Face ID / Touch ID).
CREATE TABLE sessions (
  id_hash    TEXT PRIMARY KEY,           -- sha256 of the cookie value
  member_id  INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  method     TEXT NOT NULL,              -- telegram | password | passkey
  ua         TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  seen_at    INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_member ON sessions(member_id);

CREATE TABLE login_codes (
  code_hash  TEXT PRIMARY KEY,
  member_id  INTEGER REFERENCES members(id) ON DELETE CASCADE, -- set when confirmed in the bot
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at    INTEGER
);

CREATE TABLE passwords (
  member_id    INTEGER PRIMARY KEY REFERENCES members(id) ON DELETE CASCADE,
  login        TEXT NOT NULL UNIQUE COLLATE NOCASE,
  hash         BLOB NOT NULL,
  salt         BLOB NOT NULL,
  iters        INTEGER NOT NULL,
  fails        INTEGER NOT NULL DEFAULT 0,
  locked_until INTEGER NOT NULL DEFAULT 0,
  updated_at   INTEGER NOT NULL
);

CREATE TABLE passkeys (
  id          TEXT PRIMARY KEY,          -- credential id, base64url
  member_id   INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  public_key  BLOB NOT NULL,             -- SPKI DER
  alg         INTEGER NOT NULL,          -- COSE: -7 ES256, -257 RS256
  sign_count  INTEGER NOT NULL DEFAULT 0,
  name        TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL,
  used_at     INTEGER
);
CREATE INDEX passkeys_member ON passkeys(member_id);

CREATE TABLE webauthn_challenges (
  challenge  TEXT PRIMARY KEY,
  member_id  INTEGER,                    -- registration: who adds the key; login: NULL
  kind       TEXT NOT NULL,
  expires_at INTEGER NOT NULL
);

ALTER TABLE members ADD COLUMN biometric_lock INTEGER NOT NULL DEFAULT 0;
ALTER TABLE members ADD COLUMN theme TEXT NOT NULL DEFAULT '';
