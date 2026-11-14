-- An invitation knows who sent it («Вова зовёт тебя»), and remembers that the invited one asked for a new
-- link after this one ran out (asked once, the author is not written to again).
ALTER TABLE invites ADD COLUMN author_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE invites ADD COLUMN asked_at INTEGER;
