-- One mechanism for everything a partner must answer: the time of a meeting, a move, a stop «а потом»,
-- a chore request. One proposes, the other says yes or no; the author sees «ждём», then the answer.
-- kind: time | stop | chore | cancel. ref_id: the round (time, stop, cancel) or the chore idea.
CREATE TABLE IF NOT EXISTS proposals (
  id          INTEGER PRIMARY KEY,
  space_id    INTEGER NOT NULL,
  kind        TEXT NOT NULL,
  ref_id      INTEGER NOT NULL DEFAULT 0,
  author_id   INTEGER NOT NULL,
  title       TEXT NOT NULL DEFAULT '',   -- what is proposed, in words: «Планетарий», «Забрать торт»
  sub         TEXT NOT NULL DEFAULT '',   -- the detail: «вс 15 ноя, 13:00», «до сб 21 ноя»
  payload     TEXT NOT NULL DEFAULT '{}', -- what the effect needs (slot, idea id …)
  state       TEXT NOT NULL DEFAULT 'wait', -- wait | yes | no | withdrawn
  created_at  INTEGER NOT NULL,
  answered_at INTEGER,
  reminded    INTEGER NOT NULL DEFAULT 0, -- the one reminder after a day has gone out
  author_saw  INTEGER NOT NULL DEFAULT 0  -- the author has seen the «нет»: the row leaves their home
);
CREATE INDEX IF NOT EXISTS proposals_space ON proposals(space_id, state);
