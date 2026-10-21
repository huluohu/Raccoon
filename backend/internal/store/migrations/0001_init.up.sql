-- 0001_init：Raccoon 全量初始结构（docs/03 的同源产物；禁改已应用迁移，变更走新版本文件）
-- 时间一律 INTEGER Unix 毫秒；状态字段 CHECK 约束与 task/state.go 常量同源。

CREATE TABLE IF NOT EXISTS schema_migrations (
  version    INTEGER PRIMARY KEY,
  name       TEXT    NOT NULL,
  checksum   TEXT    NOT NULL,
  applied_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
  id            TEXT PRIMARY KEY,
  username      TEXT    NOT NULL UNIQUE,
  password_hash TEXT    NOT NULL,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  last_login_at INTEGER
);

CREATE TABLE IF NOT EXISTS api_tokens (
  id           TEXT PRIMARY KEY,
  user_id      TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT    NOT NULL,
  token_hash   TEXT    NOT NULL UNIQUE,
  scope        INTEGER NOT NULL DEFAULT 1,
  expires_at   INTEGER,
  revoked_at   INTEGER,
  created_at   INTEGER NOT NULL,
  last_used_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);

CREATE TABLE IF NOT EXISTS credentials (
  id           TEXT PRIMARY KEY,
  platform     TEXT    NOT NULL,
  kind         TEXT    NOT NULL CHECK (kind IN ('cookie_file','account','token','session')),
  label        TEXT    NOT NULL,
  ciphertext   BLOB    NOT NULL,
  status       TEXT    NOT NULL DEFAULT 'active'
               CHECK (status IN ('active','expiring','expired','disabled')),
  note         TEXT,
  expires_at   INTEGER,
  last_check_at INTEGER,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_credentials_platform ON credentials(platform, kind);

CREATE TABLE IF NOT EXISTS settings (
  key            TEXT PRIMARY KEY,
  value_json     TEXT    NOT NULL,
  effective      TEXT    NOT NULL DEFAULT 'next_task'
                  CHECK (effective IN ('immediate','next_task','restart')),
  sensitive      INTEGER NOT NULL DEFAULT 0,
  locked_by_env  TEXT,
  version        INTEGER NOT NULL DEFAULT 1,
  updated_at     INTEGER NOT NULL,
  updated_by     TEXT
);

CREATE TABLE IF NOT EXISTS setting_history (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  key        TEXT    NOT NULL,
  value_json TEXT    NOT NULL,
  version    INTEGER NOT NULL,
  changed_at INTEGER NOT NULL,
  changed_by TEXT,
  note       TEXT
);
CREATE INDEX IF NOT EXISTS idx_setting_history_key ON setting_history(key, id DESC);

CREATE TABLE IF NOT EXISTS tasks (
  id             TEXT PRIMARY KEY,
  parent_id      TEXT REFERENCES tasks(id) ON DELETE CASCADE,
  kind           TEXT    NOT NULL DEFAULT 'download'
                 CHECK (kind IN ('download','record','qb','n115','postprocess')),
  source         TEXT    NOT NULL
                 CHECK (source IN ('web','telegram','api','cli','extension','subscription','live','system')),
  source_ref     TEXT,
  platform       TEXT,
  url            TEXT    NOT NULL,
  title          TEXT,
  thumbnail      TEXT,
  dedupe_key     TEXT    NOT NULL,
  status         TEXT    NOT NULL DEFAULT 'waiting'
                 CHECK (status IN ('waiting','parsing','downloading','post','done','failed','cancelled')),
  wait_reason    TEXT,
  quality        TEXT,
  save_path      TEXT,
  expected_size  INTEGER,
  downloaded_size INTEGER NOT NULL DEFAULT 0,
  speed_bps      INTEGER NOT NULL DEFAULT 0,
  progress       REAL    NOT NULL DEFAULT 0,
  resume_supported INTEGER NOT NULL DEFAULT 0,
  engine         TEXT,
  engine_version TEXT,
  config_version INTEGER NOT NULL,
  attempt        INTEGER NOT NULL DEFAULT 0,
  max_attempts   INTEGER NOT NULL DEFAULT 3,
  error_code     TEXT,
  error_message  TEXT,
  fix_hint       TEXT,
  cancel_requested INTEGER NOT NULL DEFAULT 0,
  scheduled_at   INTEGER,
  deleted_at     INTEGER,
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL,
  started_at     INTEGER,
  finished_at    INTEGER
);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status, scheduled_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_dedupe ON tasks(dedupe_key) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_active_dedupe
  ON tasks(dedupe_key) WHERE status IN ('waiting','parsing','downloading','post');
CREATE INDEX IF NOT EXISTS idx_tasks_source ON tasks(source, source_ref);
CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS task_events (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id  TEXT    NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  ts       INTEGER NOT NULL,
  level    TEXT    NOT NULL DEFAULT 'info' CHECK (level IN ('debug','info','warn','error')),
  event    TEXT    NOT NULL,
  message  TEXT,
  fields_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_task_events_task ON task_events(task_id, id);

CREATE TABLE IF NOT EXISTS task_files (
  id       TEXT PRIMARY KEY,
  task_id  TEXT    NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  path     TEXT    NOT NULL,
  size     INTEGER NOT NULL,
  mime     TEXT,
  kind     TEXT    NOT NULL DEFAULT 'other'
           CHECK (kind IN ('video','audio','image','subtitle','metadata','torrent','other')),
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_task_files_task ON task_files(task_id);

CREATE TABLE IF NOT EXISTS subscriptions (
  id             TEXT PRIMARY KEY,
  platform       TEXT    NOT NULL,
  source_type    TEXT    NOT NULL CHECK (source_type IN ('favorite','uploader','list')),
  source_id      TEXT    NOT NULL,
  source_url     TEXT    NOT NULL,
  title          TEXT,
  mode           TEXT    NOT NULL DEFAULT 'new_only'
                 CHECK (mode IN ('new_only','backfill','range')),
  range_from     INTEGER,
  range_to       INTEGER,
  quality        TEXT,
  save_rule      TEXT,
  interval_min   INTEGER NOT NULL DEFAULT 30,
  enabled        INTEGER NOT NULL DEFAULT 1,
  credential_id  TEXT REFERENCES credentials(id) ON DELETE SET NULL,
  last_checked_at INTEGER,
  last_item_id   TEXT,
  next_check_at  INTEGER NOT NULL,
  error_note     TEXT,
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL,
  UNIQUE (platform, source_type, source_id)
);

CREATE TABLE IF NOT EXISTS subscription_items (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  subscription_id TEXT   NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
  item_id        TEXT    NOT NULL,
  title          TEXT,
  task_id        TEXT REFERENCES tasks(id) ON DELETE SET NULL,
  status         TEXT    NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','queued','downloaded','skipped','failed')),
  discovered_at  INTEGER NOT NULL,
  UNIQUE (subscription_id, item_id)
);
CREATE INDEX IF NOT EXISTS idx_sub_items_status ON subscription_items(subscription_id, status);

CREATE TABLE IF NOT EXISTS live_sources (
  id           TEXT PRIMARY KEY,
  platform     TEXT    NOT NULL,
  room_id      TEXT    NOT NULL,
  room_url     TEXT    NOT NULL,
  title        TEXT,
  credential_id TEXT REFERENCES credentials(id) ON DELETE SET NULL,
  enabled      INTEGER NOT NULL DEFAULT 1,
  quality      TEXT,
  record_plan  TEXT    NOT NULL DEFAULT 'manual'
               CHECK (record_plan IN ('manual','always','scheduled')),
  schedule_json TEXT,
  segment_min  INTEGER NOT NULL DEFAULT 60,
  check_interval_sec INTEGER NOT NULL DEFAULT 60,
  status       TEXT    NOT NULL DEFAULT 'offline'
               CHECK (status IN ('offline','live','recording','error')),
  last_checked_at INTEGER,
  last_live_at INTEGER,
  error_note   TEXT,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  UNIQUE (platform, room_id)
);

CREATE TABLE IF NOT EXISTS live_recordings (
  id         TEXT PRIMARY KEY,
  source_id  TEXT    NOT NULL REFERENCES live_sources(id) ON DELETE CASCADE,
  task_id    TEXT REFERENCES tasks(id) ON DELETE SET NULL,
  started_at INTEGER NOT NULL,
  ended_at   INTEGER,
  segments   INTEGER NOT NULL DEFAULT 0,
  size       INTEGER NOT NULL DEFAULT 0,
  status     TEXT    NOT NULL DEFAULT 'recording'
             CHECK (status IN ('recording','completed','failed')),
  error_note TEXT
);
CREATE INDEX IF NOT EXISTS idx_live_rec_source ON live_recordings(source_id, started_at DESC);

CREATE TABLE IF NOT EXISTS qb_torrents (
  id         TEXT PRIMARY KEY,
  task_id    TEXT    NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  qb_hash    TEXT    NOT NULL,
  category   TEXT,
  save_path  TEXT,
  status     TEXT    NOT NULL,
  progress   REAL    NOT NULL DEFAULT 0,
  added_at   INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS n115_offline (
  id            TEXT PRIMARY KEY,
  task_id       TEXT    NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  remote_task_id TEXT,
  target_dir    TEXT,
  info_hash     TEXT,
  status        TEXT    NOT NULL DEFAULT 'submitted'
                CHECK (status IN ('submitted','querying','done','failed')),
  note          TEXT,
  updated_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS media_files (
  id          TEXT PRIMARY KEY,
  task_id     TEXT REFERENCES tasks(id) ON DELETE SET NULL,
  subscription_id TEXT REFERENCES subscriptions(id) ON DELETE SET NULL,
  path        TEXT    NOT NULL UNIQUE,
  name        TEXT    NOT NULL,
  size        INTEGER NOT NULL,
  mime        TEXT,
  duration    INTEGER,
  platform    TEXT,
  uploader    TEXT,
  created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_media_platform ON media_files(platform, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_media_uploader  ON media_files(uploader);

CREATE TABLE IF NOT EXISTS notifications (
  id         TEXT PRIMARY KEY,
  kind       TEXT    NOT NULL
             CHECK (kind IN ('task_done','task_failed','sub_new','live_start','disk_warn','system')),
  severity   TEXT    NOT NULL DEFAULT 'info'
             CHECK (severity IN ('info','success','warning','error')),
  title      TEXT    NOT NULL,
  body       TEXT,
  task_id    TEXT REFERENCES tasks(id) ON DELETE SET NULL,
  read       INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notif_unread ON notifications(read, created_at DESC);
