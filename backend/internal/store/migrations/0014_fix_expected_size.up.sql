-- 0014：校正完成态任务的预期大小（修复 DASH 双流下载的历史错值）。
--
-- 历史 bug：yt-dlp 下载 DASH 源时，视频流（分片下载）报 progress.total_bytes=NA，
-- 音频流（单文件 range 请求）报的是**该音频流自己**的大小。原实现「取第一帧有
-- total 的进度行」当 expected_size，恰好取到的是音频流，于是 DASH 任务的预期大小
-- 变成了音频流大小（实测 13.27 MiB），而真实文件是视频+音频合并后的 210.58 MiB——
-- 任务详情显示「210.6 MB / 13.3 MB」，分母比分子还小。
--
-- 修复：下载中按流累加各流 total（task/streams.go），完成态用真实文件大小校正。
-- 本迁移把**历史**完成态任务的错值一并校正：完成态的 downloaded_size 就是真实
-- 文件大小（finish 用产物实际字节数回写），用它校正 expected_size 后二者必然一致。
UPDATE tasks
SET expected_size = downloaded_size
WHERE status = 'done'
  AND expected_size IS NOT NULL
  AND expected_size <> downloaded_size;
