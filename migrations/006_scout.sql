-- Скаут: идеи, которые нашёл агент по афишам, и почему он их выбрал. Отметки на них — обучение вкусам.
ALTER TABLE ideas ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE ideas ADD COLUMN why TEXT NOT NULL DEFAULT '';
CREATE TABLE scout_runs (
  id         INTEGER PRIMARY KEY,
  space_id   INTEGER NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  picks      INTEGER NOT NULL,
  prices     INTEGER NOT NULL DEFAULT 0,
  note       TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
