-- «А потом?»: продолжение встречи (музей → ужин). Время не хранится — считается от конца предыдущего пункта,
-- так что перенос встречи сдвигает весь маршрут. Каждый пункт подтверждает второй партнёр.
CREATE TABLE round_extras (
  id          INTEGER PRIMARY KEY,
  round_id    INTEGER NOT NULL REFERENCES rounds(id) ON DELETE CASCADE,
  idea_id     INTEGER NOT NULL REFERENCES ideas(id) ON DELETE CASCADE,
  pos         INTEGER NOT NULL,
  proposed_by INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
  state       TEXT NOT NULL DEFAULT 'proposed', -- proposed | accepted | declined
  created_at  INTEGER NOT NULL
);
CREATE INDEX round_extras_round ON round_extras(round_id);
