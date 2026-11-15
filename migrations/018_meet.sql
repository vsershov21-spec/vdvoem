-- «Я занят в этот день»: marked by hand in Планы, one tap, no calendar link needed.
CREATE TABLE IF NOT EXISTS busy_days (
  member_id INTEGER NOT NULL,
  day       TEXT NOT NULL,     -- YYYY-MM-DD in the couple's time zone
  PRIMARY KEY (member_id, day)
);
-- «Общее время»: when each of the two is free (a day and a part of it), marked ahead of any idea.
CREATE TABLE IF NOT EXISTS free_slots (
  member_id INTEGER NOT NULL,
  day       TEXT NOT NULL,
  part      INTEGER NOT NULL,  -- 0 morning · 1 day · 2 evening
  PRIMARY KEY (member_id, day, part)
);
