-- 0002: dedupe 唯一索引排除已软删除的任务。
-- 0001 的 idx_tasks_active_dedupe 只按 status 过滤，导致「已软删除但状态仍是
-- waiting/downloading」的任务继续占用 dedupe_key，用户重新提交同一链接时
-- INSERT 撞唯一索引。这里重建为「deleted_at IS NULL 且活跃态」才占位。
DROP INDEX IF EXISTS idx_tasks_active_dedupe;
CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_active_dedupe
  ON tasks(dedupe_key)
  WHERE deleted_at IS NULL AND status IN ('waiting','parsing','downloading','post');