-- 0003：媒体库 kind 列 + 凭据/通知/订阅索引（docs/03 补充；0001 已应用不可改，改走新版本）
-- kind 取值：video/audio/image/subtitle/other（由 media 库登记时按 mime/扩展名判定）

ALTER TABLE media_files ADD COLUMN kind TEXT NOT NULL DEFAULT 'other';

CREATE INDEX IF NOT EXISTS idx_media_kind ON media_files(kind, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_media_name ON media_files(name);
CREATE INDEX IF NOT EXISTS idx_media_task ON media_files(task_id);
