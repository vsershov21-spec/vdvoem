-- A «подборка»: what the assistant found for a request, kept as a list to open, go through, mark and send to
-- the partner — the chat shows the best few, the list holds them all.
CREATE TABLE picks (
  id         INTEGER PRIMARY KEY,
  space_id   INTEGER NOT NULL,
  member_id  INTEGER NOT NULL,
  title      TEXT NOT NULL,
  ids        TEXT NOT NULL,   -- JSON array of idea ids, best first
  created_at INTEGER NOT NULL
);
CREATE INDEX picks_space ON picks(space_id, id);
