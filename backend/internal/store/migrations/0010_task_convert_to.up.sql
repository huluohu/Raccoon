-- 0010：任务产物格式转换（docs/05 §4 post 阶段新增动作）。
-- convert_to 空 = 保持源格式（存量行为零变化）；'mp4' / 'mp3' 由
-- ffmpeg 在 post 阶段 remux（容器兼容时无损、秒级）或转码完成。
ALTER TABLE tasks ADD COLUMN convert_to TEXT;
