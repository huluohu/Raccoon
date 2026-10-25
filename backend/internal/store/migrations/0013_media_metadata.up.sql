-- 0013：媒体库封面与可编辑元数据。
--
-- 背景：媒体库此前只存「文件名 + 大小 + 平台 + 时长」，视频没有封面，
-- 标题/简介/标签也无法编辑或刮削，只能靠重新下载换元数据。
--
-- cover_path：封面图路径（jpg，与视频同目录或 media 目录下）。
--   空串 = 无封面，前端回退到类型占位。
-- title：展示标题（可编辑；空串时前端回退到 name）。
-- description：简介 / 描述（可编辑，也可由 AI 刮削填充）。
-- tags：标签，逗号分隔（可编辑，也可由 AI 刮削填充）。
ALTER TABLE media_files ADD COLUMN cover_path TEXT NOT NULL DEFAULT '';
ALTER TABLE media_files ADD COLUMN title TEXT NOT NULL DEFAULT '';
ALTER TABLE media_files ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE media_files ADD COLUMN tags TEXT NOT NULL DEFAULT '';
