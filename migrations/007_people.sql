-- У каждого человека своё: память помощника и цели для скаута. Плюс примеры для дообучения модели
-- и полка «анонсов» (события на 1–3 месяца вперёд).
CREATE TABLE memories (
  id         INTEGER PRIMARY KEY,
  space_id   INTEGER NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  member_id  INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  text       TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX memories_member ON memories(member_id);
CREATE TABLE scout_goals (
  id         INTEGER PRIMARY KEY,
  space_id   INTEGER NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  member_id  INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  text       TEXT NOT NULL,
  done       INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE INDEX scout_goals_space ON scout_goals(space_id, done);
CREATE TABLE llm_examples (
  id         INTEGER PRIMARY KEY,
  member_id  INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  input      TEXT NOT NULL,   -- the question
  calls      TEXT NOT NULL,   -- tool calls the model made, JSON
  answer     TEXT NOT NULL,
  accepted   INTEGER NOT NULL DEFAULT 0, -- a proposed button was pressed
  created_at INTEGER NOT NULL
);
ALTER TABLE ideas ADD COLUMN horizon TEXT NOT NULL DEFAULT '';  -- scout: week | later
ALTER TABLE ideas ADD COLUMN on_sale INTEGER;                   -- scout: when tickets go on sale
ALTER TABLE assistant_actions ADD COLUMN example_id INTEGER NOT NULL DEFAULT 0;
