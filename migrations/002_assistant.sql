-- Помощник: переписка внутри пространства пары. Каждый видит свою ветку; данные пары общие.
CREATE TABLE assistant_messages (
  id         INTEGER PRIMARY KEY,
  space_id   INTEGER NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  member_id  INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  role       TEXT NOT NULL CHECK (role IN ('user','assistant')),
  body       TEXT NOT NULL,           -- JSON: {"text":..., "cards":[...], "actions":[...]}
  created_at INTEGER NOT NULL
);
CREATE INDEX assistant_messages_member ON assistant_messages(member_id, id);

-- Предложенные помощником действия: выполняются только по нажатию человека, один раз.
CREATE TABLE assistant_actions (
  id         TEXT PRIMARY KEY,         -- случайный токен
  space_id   INTEGER NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  member_id  INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  payload    TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  used_at    INTEGER
);

-- Бот сам предлагает идею: по оценке, случайно или через неделю.
ALTER TABLE spaces ADD COLUMN suggest_mode TEXT NOT NULL DEFAULT 'mix' CHECK (suggest_mode IN ('best','random','mix','off'));
ALTER TABLE spaces ADD COLUMN suggest_weekday INTEGER NOT NULL DEFAULT 3; -- среда
ALTER TABLE spaces ADD COLUMN suggest_hour INTEGER NOT NULL DEFAULT 13;
