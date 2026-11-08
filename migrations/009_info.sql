-- What an event is: a short description, genres, who plays, age — from the post or the web (the scout).
-- One JSON column: the assistant tells about the event and explains whether it suits each of the two.
ALTER TABLE ideas ADD COLUMN info TEXT NOT NULL DEFAULT '';
