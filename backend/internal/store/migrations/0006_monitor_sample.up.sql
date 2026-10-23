-- 0006：监控历史聚合（monitor_sample）+ 任务存储路径留痕（tasks.route_path）。
-- monitor_sample：1min 聚合一行，保留 7 天（sampler 每次聚合写入口删除过期行）。
-- route_path：存储策略「走了哪条路」的人类可读链路，如
--   "本地下载" / "qB" / "115 优先 → 115 提交成功" / "115 失败 → 转 qB"。
CREATE TABLE IF NOT EXISTS monitor_sample (
    ts      INTEGER PRIMARY KEY,          -- Unix 秒，聚合桶起点
    cpu     REAL NOT NULL,
    memory  REAL NOT NULL,
    disk    REAL NOT NULL,
    net_rx  REAL NOT NULL,
    net_tx  REAL NOT NULL
);

ALTER TABLE tasks ADD COLUMN route_path TEXT NOT NULL DEFAULT '';
