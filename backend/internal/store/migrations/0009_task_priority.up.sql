-- 0009：任务优先级（docs/05 §3 调度：waiting 队列按 priority DESC, created_at ASC 领取）。
-- 0 = 普通优先（存量行为零变化）；>0 = 插队（数值越大越先领）。
-- 只影响领取顺序，不影响并发闸门与公平性以外的任何语义。
ALTER TABLE tasks ADD COLUMN priority INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_tasks_waiting_priority
    ON tasks(status, priority DESC, created_at);
