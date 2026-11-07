-- «Вдвоём» — схема SQLite. Деньги в копейках, время в UTC (unix seconds).
CREATE TABLE IF NOT EXISTS spaces (
  id            INTEGER PRIMARY KEY,
  city          TEXT NOT NULL DEFAULT 'Москва',
  tz            TEXT NOT NULL DEFAULT 'Europe/Moscow',
  plan_weekday  INTEGER NOT NULL DEFAULT 4,      -- 0 = Sunday … 6; 4 = Thursday
  plan_hour     INTEGER NOT NULL DEFAULT 19,
  budget_kop    INTEGER NOT NULL DEFAULT 0,
  status        TEXT NOT NULL DEFAULT 'solo',    -- solo | paired | frozen
  created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS members (
  id          INTEGER PRIMARY KEY,
  space_id    INTEGER NOT NULL REFERENCES spaces(id),
  tg_user_id  INTEGER NOT NULL UNIQUE,
  chat_id     INTEGER NOT NULL,
  name        TEXT NOT NULL,
  color       TEXT NOT NULL CHECK (color IN ('a','b')),
  photo_url   TEXT NOT NULL DEFAULT '',
  joined_at   INTEGER NOT NULL,
  UNIQUE (space_id, color)
);

CREATE TABLE IF NOT EXISTS invites (
  code_hash   TEXT PRIMARY KEY,
  space_id    INTEGER NOT NULL REFERENCES spaces(id),
  expires_at  INTEGER NOT NULL,
  used_at     INTEGER
);

CREATE TABLE IF NOT EXISTS ideas (
  id           INTEGER PRIMARY KEY,
  space_id     INTEGER NOT NULL REFERENCES spaces(id),
  author_id    INTEGER NOT NULL REFERENCES members(id),
  title        TEXT NOT NULL,
  kind         TEXT NOT NULL DEFAULT 'leisure',
  category     TEXT NOT NULL DEFAULT 'other',
  url          TEXT NOT NULL DEFAULT '',
  address      TEXT NOT NULL DEFAULT '',
  image_url    TEXT NOT NULL DEFAULT '',
  note         TEXT NOT NULL DEFAULT '',
  price_min    INTEGER NOT NULL DEFAULT 0,
  price_max    INTEGER NOT NULL DEFAULT 0,
  duration_min INTEGER NOT NULL DEFAULT 0,
  starts_at    INTEGER,
  ends_at      INTEGER,
  deadline     INTEGER,
  assignee     TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'active',
  fingerprint  TEXT NOT NULL DEFAULT '',
  wish_a       TEXT NOT NULL DEFAULT '',
  wish_b       TEXT NOT NULL DEFAULT '',
  again_yes    INTEGER NOT NULL DEFAULT 0,
  again_no     INTEGER NOT NULL DEFAULT 0,
  ever_shown   INTEGER NOT NULL DEFAULT 0,
  last_met_at  INTEGER,
  parsed       INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS ideas_space ON ideas(space_id, status);
CREATE INDEX IF NOT EXISTS ideas_fp ON ideas(space_id, fingerprint);

CREATE TABLE IF NOT EXISTS rounds (
  id         INTEGER PRIMARY KEY,
  space_id   INTEGER NOT NULL REFERENCES spaces(id),
  state      TEXT NOT NULL,
  version    INTEGER NOT NULL DEFAULT 0,
  body       TEXT NOT NULL,          -- domain.Round as JSON
  starts_at  INTEGER,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS rounds_space ON rounds(space_id, id DESC);
-- one open round per space
CREATE UNIQUE INDEX IF NOT EXISTS rounds_one_open ON rounds(space_id) WHERE state IN ('time','vote','confirm');

CREATE TABLE IF NOT EXISTS photos (
  id        INTEGER PRIMARY KEY,
  round_id  INTEGER NOT NULL REFERENCES rounds(id),
  member_id INTEGER NOT NULL REFERENCES members(id),
  file_id   TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS jobs (
  id          INTEGER PRIMARY KEY,
  kind        TEXT NOT NULL,
  run_at      INTEGER NOT NULL,
  payload     TEXT NOT NULL DEFAULT '{}',
  dedupe_key  TEXT UNIQUE,
  attempts    INTEGER NOT NULL DEFAULT 0,
  done_at     INTEGER,
  last_error  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS jobs_due ON jobs(done_at, run_at);

CREATE TABLE IF NOT EXISTS tg_updates (
  update_id  INTEGER PRIMARY KEY,
  seen_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS ai_log (
  id        INTEGER PRIMARY KEY,
  task      TEXT NOT NULL,
  provider  TEXT NOT NULL,
  input     TEXT NOT NULL,
  output    TEXT NOT NULL,
  ok        INTEGER NOT NULL,
  ms        INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS schema_version (v INTEGER NOT NULL);
