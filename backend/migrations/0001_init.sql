PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version       INTEGER PRIMARY KEY,
  applied_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS devices (
  id            TEXT PRIMARY KEY,
  ip_address    TEXT NOT NULL UNIQUE,
  mac_address   TEXT,
  display_name  TEXT NOT NULL,
  hostname      TEXT,
  state         TEXT NOT NULL CHECK (state IN ('active', 'inactive', 'unknown')),
  first_seen_at INTEGER NOT NULL,
  last_seen_at  INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_devices_state_last_seen
  ON devices(state, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS applications (
  id            TEXT PRIMARY KEY,
  observed_host TEXT NOT NULL UNIQUE COLLATE NOCASE,
  match_kind    TEXT NOT NULL CHECK (match_kind IN ('domain', 'suffix', 'keyword')),
  match_value   TEXT NOT NULL,
  state         TEXT NOT NULL CHECK (state IN ('active', 'inactive')),
  first_seen_at INTEGER NOT NULL,
  last_seen_at  INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_applications_state_last_seen
  ON applications(state, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS device_applications (
  id                 TEXT PRIMARY KEY,
  device_id          TEXT NOT NULL REFERENCES devices(id) ON DELETE RESTRICT,
  application_id     TEXT NOT NULL REFERENCES applications(id) ON DELETE RESTRICT,
  network            TEXT NOT NULL CHECK (network IN ('tcp', 'udp', 'quic', 'unknown')),
  transport_hint     TEXT,
  destination_ip     TEXT,
  destination_port   INTEGER,
  state              TEXT NOT NULL CHECK (state IN ('active', 'inactive')),
  active_connections INTEGER NOT NULL DEFAULT 0 CHECK (active_connections >= 0),
  first_seen_at      INTEGER NOT NULL,
  last_seen_at       INTEGER NOT NULL,
  inactive_at        INTEGER,
  UNIQUE(device_id, application_id, network)
);
CREATE INDEX IF NOT EXISTS idx_device_applications_device_state
  ON device_applications(device_id, state, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_device_applications_app_state
  ON device_applications(application_id, state, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS connection_samples (
  connection_id         TEXT PRIMARY KEY,
  device_application_id TEXT NOT NULL REFERENCES device_applications(id) ON DELETE CASCADE,
  source_ip             TEXT NOT NULL,
  destination_ip        TEXT,
  observed_host         TEXT,
  network               TEXT NOT NULL,
  opened_at             INTEGER NOT NULL,
  last_observed_at      INTEGER NOT NULL,
  closed_at             INTEGER,
  upload_bytes          INTEGER NOT NULL DEFAULT 0,
  download_bytes        INTEGER NOT NULL DEFAULT 0,
  proxy_chain_json      TEXT NOT NULL DEFAULT '[]',
  matched_rule          TEXT,
  matched_rule_payload  TEXT
);
CREATE INDEX IF NOT EXISTS idx_connection_samples_da_last_observed
  ON connection_samples(device_application_id, last_observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_connection_samples_closed_at
  ON connection_samples(closed_at);

CREATE TABLE IF NOT EXISTS canvases (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  revision      INTEGER NOT NULL DEFAULT 0,
  is_default    INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_canvases_one_default
  ON canvases(is_default) WHERE is_default = 1;

CREATE TABLE IF NOT EXISTS canvas_nodes (
  id            TEXT PRIMARY KEY,
  canvas_id     TEXT NOT NULL REFERENCES canvases(id) ON DELETE CASCADE,
  node_kind     TEXT NOT NULL CHECK (node_kind IN ('source', 'filter', 'target')),
  resource_id   TEXT NOT NULL,
  position_x    REAL NOT NULL,
  position_y    REAL NOT NULL,
  data_json     TEXT NOT NULL DEFAULT '{}',
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  UNIQUE(canvas_id, node_kind, resource_id)
);

CREATE TABLE IF NOT EXISTS canvas_edges (
  id             TEXT PRIMARY KEY,
  canvas_id      TEXT NOT NULL REFERENCES canvases(id) ON DELETE CASCADE,
  source_node_id TEXT NOT NULL REFERENCES canvas_nodes(id) ON DELETE CASCADE,
  target_node_id TEXT NOT NULL REFERENCES canvas_nodes(id) ON DELETE CASCADE,
  edge_kind      TEXT NOT NULL CHECK (edge_kind IN ('source_to_filter', 'filter_to_target')),
  created_at     INTEGER NOT NULL,
  UNIQUE(canvas_id, source_node_id, target_node_id)
);
CREATE INDEX IF NOT EXISTS idx_canvas_edges_canvas
  ON canvas_edges(canvas_id);

CREATE TABLE IF NOT EXISTS compilation_revisions (
  id                 TEXT PRIMARY KEY,
  canvas_id          TEXT NOT NULL REFERENCES canvases(id) ON DELETE RESTRICT,
  canvas_revision    INTEGER NOT NULL,
  status             TEXT NOT NULL CHECK (status IN ('draft', 'validated', 'applied', 'failed', 'rolled_back')),
  generated_yaml     TEXT,
  mihomo_config_hash TEXT,
  error_message      TEXT,
  created_at         INTEGER NOT NULL,
  applied_at         INTEGER
);
CREATE INDEX IF NOT EXISTS idx_compilation_revisions_canvas_revision
  ON compilation_revisions(canvas_id, canvas_revision DESC);
