-- «Важно»: one person asks the other to answer about an idea quickly (tickets may go). The partner is
-- reminded until they answer: yes (a meeting is being planned), no, or the event passes.
CREATE TABLE urgent (
  idea_id    INTEGER PRIMARY KEY REFERENCES ideas(id) ON DELETE CASCADE,
  space_id   INTEGER NOT NULL,
  by_member  INTEGER NOT NULL,
  note       TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  next_at    INTEGER NOT NULL,  -- the next reminder
  nudges     INTEGER NOT NULL DEFAULT 0,
  done_at    INTEGER,           -- answered or closed
  answer     TEXT NOT NULL DEFAULT ''  -- yes | no | cancelled | expired
);
CREATE INDEX urgent_due ON urgent(next_at) WHERE done_at IS NULL;
