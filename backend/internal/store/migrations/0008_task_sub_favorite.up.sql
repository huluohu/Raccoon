-- 0008：收藏下载快照标记（docs/15 §3.3 口径：删订阅不影响历史统计）。
-- tasks.sub_favorite = 1 表示「这条任务当时是从收藏类订阅下发的」。
-- 正常在册的收藏订阅靠 JOIN subscriptions 即可识别；只有**删除订阅前**
-- 才把标记落到任务行上——否则删订阅那一刻历史统计会凭空少一批（数据说谎）。
ALTER TABLE tasks ADD COLUMN sub_favorite INTEGER NOT NULL DEFAULT 0;
