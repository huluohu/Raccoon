package task

// streams.go：多流（DASH）下载的体积口径。
//
// 背景（2026-10-25 实测定位）：yt-dlp 下载 YouTube 这类 DASH 源时，
// 视频流与音频流是**两个独立请求**：
//
//   - 视频流（分片下载）：progress.total_bytes 期间为 NA，末尾才揭晓；
//   - 音频流（单文件 range）：progress.total_bytes 从头就准确，值是**该音频流自己**的大小。
//
// 原实现「取第一帧有 total 的进度行」当 expected_size，于是 DASH 任务的预期大小
// 变成了音频流大小（实测 13.27 MiB），而真实文件是视频+音频合并后的 210.58 MiB——
// 任务详情显示「210.6 MB / 13.3 MB」，分母比分子还小。
//
// 现在按「流」去重累加：每个不同的 total 值视为一个流，sum 即总体积。

// sumStreamTotals 累加各流的 total（去重后求和）。
//
// 单流下载：只有一个 total → 结果就是它本身，精确。
// 多流下载：各流 total 之和 ≈ 合并后大小（差值来自容器封装开销，通常 <1%）。
// 播放列表（多条目）：各条目 total 之和也会被算进来——但播放列表的 expected_size
// 本就不可知，调用方按 itemCount 判断，不使用这个值。
func sumStreamTotals(totals map[int64]bool) int64 {
	var sum int64
	for t := range totals {
		sum += t
	}
	return sum
}

// expectedSizeFor 给 SSE 帧用的 expected_size（多条目播放列表返回 0 = 未知）。
//
// 0 的语义是「未知」：前端据此显示不确定条纹而不是假的百分比。
func expectedSizeFor(totals map[int64]bool, itemCount int) int64 {
	if itemCount > 1 {
		return 0 // 播放列表总大小不可知（禁估算）
	}
	return sumStreamTotals(totals)
}
