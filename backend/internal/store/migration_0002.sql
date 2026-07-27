CREATE TABLE IF NOT EXISTS compilation_rollbacks (
  id                    TEXT PRIMARY KEY,
  compilation_id        TEXT NOT NULL UNIQUE REFERENCES compilation_revisions(id) ON DELETE CASCADE,
  prior_config_hash     TEXT NOT NULL,
  candidate_config_hash TEXT NOT NULL,
  backup_path           TEXT NOT NULL,
  status                TEXT NOT NULL CHECK (status IN ('not_needed', 'restored', 'rollback_failed')),
  error_message         TEXT,
  created_at            INTEGER NOT NULL,
  restored_at           INTEGER
);
CREATE INDEX IF NOT EXISTS idx_compilation_rollbacks_compilation
  ON compilation_rollbacks(compilation_id);
