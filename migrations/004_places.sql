-- «Где именно»: конкретные места для идеи. Добавляют оба (или помощник), один вариант выбирается для встречи.
CREATE TABLE idea_options (
  id          INTEGER PRIMARY KEY,
  idea_id     INTEGER NOT NULL REFERENCES ideas(id) ON DELETE CASCADE,
  member_id   INTEGER REFERENCES members(id) ON DELETE SET NULL, -- NULL: suggested by the assistant
  title       TEXT NOT NULL,
  url         TEXT NOT NULL DEFAULT '',
  address     TEXT NOT NULL DEFAULT '',
  image_url   TEXT NOT NULL DEFAULT '',
  note        TEXT NOT NULL DEFAULT '',
  price_kop   INTEGER NOT NULL DEFAULT 0,
  parsed      INTEGER NOT NULL DEFAULT 0,
  created_at  INTEGER NOT NULL
);
CREATE INDEX idea_options_idea ON idea_options(idea_id);
ALTER TABLE ideas ADD COLUMN chosen_option INTEGER;

-- which bot message shows which idea, so a reply with a link lands on the right card
CREATE TABLE bot_cards (
  chat_id    INTEGER NOT NULL,
  message_id INTEGER NOT NULL,
  idea_id    INTEGER NOT NULL REFERENCES ideas(id) ON DELETE CASCADE,
  PRIMARY KEY (chat_id, message_id)
);
