-- How each understood request ended: pressed (a button from the answer), clarified (the other reading was
-- chosen — this one was wrong), rephrased (asked again in other words right away), labeled (fixed by hand).
-- Misses are what the model is retrained on.
ALTER TABLE llm_examples ADD COLUMN outcome TEXT NOT NULL DEFAULT '';
ALTER TABLE llm_examples ADD COLUMN src TEXT NOT NULL DEFAULT '';
