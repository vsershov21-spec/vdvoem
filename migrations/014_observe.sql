-- What the assistant did with each request, visible to the owner (admin): time, model, buttons, errors.
ALTER TABLE llm_examples ADD COLUMN ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE llm_examples ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE llm_examples ADD COLUMN buttons TEXT NOT NULL DEFAULT '';
ALTER TABLE llm_examples ADD COLUMN cards INTEGER NOT NULL DEFAULT 0;
ALTER TABLE llm_examples ADD COLUMN err TEXT NOT NULL DEFAULT '';
-- How long the app takes to open on the phones: every boot stage, kept 30 days (not in memory: a deploy wiped it).
CREATE TABLE client_boots (
  id      INTEGER PRIMARY KEY,
  boot    TEXT NOT NULL DEFAULT '',  -- one opening of the app
  stage   TEXT NOT NULL,
  msg     TEXT NOT NULL DEFAULT '',
  ms      INTEGER NOT NULL DEFAULT 0,
  at      INTEGER NOT NULL
);
CREATE INDEX client_boots_at ON client_boots(at);
-- the server time of every API request, sampled (one in four) and kept 7 days
CREATE TABLE route_times (
  route TEXT NOT NULL,
  ms    INTEGER NOT NULL,
  at    INTEGER NOT NULL
);
CREATE INDEX route_times_at ON route_times(at);
