-- App settings changed at run time (e.g. which local model understands the assistant's requests).
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
