-- 0012：AI 总结与分析（支持持久化视频/媒体的结构化摘要与看点）。
ALTER TABLE media_files ADD COLUMN summary TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN summary TEXT NOT NULL DEFAULT '';
