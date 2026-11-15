-- Chores get their own table: who, when, repeat, and a state for every place a chore can be
-- (open, asked, declined, done, deleted). Until now a chore was an idea of kind 'chore'.
CREATE TABLE IF NOT EXISTS chores (
  id          INTEGER PRIMARY KEY,
  space_id    INTEGER NOT NULL,
  author_id   INTEGER NOT NULL,
  assignee_id INTEGER NOT NULL DEFAULT 0,  -- 0: anyone
  title       TEXT NOT NULL,
  note        TEXT NOT NULL DEFAULT '',
  due_at      INTEGER,
  due_time    INTEGER NOT NULL DEFAULT 0,  -- the deadline has a time of day
  repeat      TEXT NOT NULL DEFAULT '',    -- '' | week | month
  state       TEXT NOT NULL DEFAULT 'open',
  proposal_id INTEGER NOT NULL DEFAULT 0,
  done_by     INTEGER NOT NULL DEFAULT 0,
  done_at     INTEGER,
  created_at  INTEGER NOT NULL,
  from_idea   INTEGER NOT NULL DEFAULT 0   -- copied from an old chore-idea (copied once)
);
CREATE INDEX IF NOT EXISTS chores_space ON chores(space_id, state);

-- the chores that already exist move over; the ideas stay where they are for the current screens
INSERT INTO chores(space_id, author_id, assignee_id, title, due_at, state, created_at, from_idea)
SELECT i.space_id, i.author_id,
       COALESCE((SELECT m.id FROM members m WHERE m.space_id = i.space_id AND m.color = i.assignee), 0),
       i.title, i.deadline, 'open', i.created_at, i.id
FROM ideas i
WHERE i.kind = 'chore' AND i.status IN ('active', 'draft')
  AND NOT EXISTS (SELECT 1 FROM chores c WHERE c.from_idea = i.id);

-- Settings: mine and the couple's (the old «settings» table keeps the server's own switches)
CREATE TABLE IF NOT EXISTS member_settings (
  member_id INTEGER NOT NULL,
  key       TEXT NOT NULL,
  value     TEXT NOT NULL,
  PRIMARY KEY (member_id, key)
);
CREATE TABLE IF NOT EXISTS space_settings (
  space_id INTEGER NOT NULL,
  key      TEXT NOT NULL,
  value    TEXT NOT NULL,
  PRIMARY KEY (space_id, key)
);
