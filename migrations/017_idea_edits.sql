-- Who filled what in an idea: a field a person typed is theirs («я» / «Настя»); the rest came from the scout
-- or from the page behind the link. One row per field, the last editor wins.
CREATE TABLE IF NOT EXISTS idea_edits (
  idea_id   INTEGER NOT NULL,
  field     TEXT NOT NULL,   -- when | where | price | about | note | title, or _enrich (asked the scout to fill the gaps)
  member_id INTEGER NOT NULL,
  at        INTEGER NOT NULL,
  PRIMARY KEY (idea_id, field)
);
