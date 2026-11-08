-- Вопросы к скауту, на которые нельзя ответить сразу («сколько стоит…», «есть ли билеты…»):
-- агент отвечает при следующем запуске, ответ приходит спросившему в бот.
CREATE TABLE scout_questions (
  id          INTEGER PRIMARY KEY,
  space_id    INTEGER NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  member_id   INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  idea_id     INTEGER,
  text        TEXT NOT NULL,
  answer      TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL,
  answered_at INTEGER
);
CREATE INDEX scout_questions_open ON scout_questions(space_id, answered_at);
