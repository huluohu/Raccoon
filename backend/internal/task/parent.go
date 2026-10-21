package task

import "time"

// parent.go：父子任务聚合（docs/05 §5）。
//
// 父进度 = Σ子 downloaded_size / Σ子 expected_size（任一子 expected_size 未知 → 显示「N/M 项」）。
// 父终态：全部子终态 → done；存在失败子 → done 但带 failed_children=N（父卡可一键重试失败项）。

// aggregateParent 刷新父任务进度与终态（子任务终态变化时调用）。
func (m *Manager) aggregateParent(parentID *string) {
	if parentID == nil || *parentID == "" {
		return
	}
	children, err := m.st.ListChildren(*parentID)
	if err != nil || len(children) == 0 {
		return
	}
	var sumDownloaded, sumExpected int64
	unknownSize := false
	terminal := 0
	failed := 0
	doneCount := 0
	for _, c := range children {
		sumDownloaded += c.DownloadedSize
		if c.ExpectedSize != nil {
			sumExpected += *c.ExpectedSize
		} else {
			unknownSize = true
		}
		switch c.Status {
		case "failed":
			failed++
			terminal++
		case "done":
			doneCount++
			terminal++
		case "cancelled":
			terminal++
		default:
			// 非终态：不计入
		}
	}
	fields := map[string]any{
		"downloaded_size": sumDownloaded,
	}
	switch {
	case !unknownSize && sumExpected > 0:
		// 有体积信息：按字节比例（最精确）。
		fields["progress"] = float64(sumDownloaded) * 100 / float64(sumExpected)
	case len(children) > 0:
		// 无体积信息（B 站等平台 yt-dlp 不返回 expected_size）：
		// 退化为「完成项占比」。否则父任务进度会永远停在 0，
		// 即使全部子任务都已完成，UI 上仍显示未开始。
		fields["progress"] = float64(doneCount+failed) * 100 / float64(len(children))
	}
	if terminal == len(children) {
		// 全部子任务终态 → 父 done（带失败子计数）。
		fields["status"] = "done"
		fields["finished_at"] = time.Now().UnixMilli()
		fields["speed_bps"] = 0
	}
	_ = m.st.UpdateTaskFields(*parentID, fields, "")
	if terminal == len(children) {
		_ = m.st.AppendEvent(*parentID, "info", "done",
			pluralFailed(failed, len(children)), "")
		m.hub.Broadcast("task.status", map[string]any{
			"task_id": *parentID, "status": "done", "child_total": len(children), "child_failed": failed,
		})
	} else {
		m.hub.Broadcast("task.progress", map[string]any{
			"task_id": *parentID, "progress": fields["progress"], "downloaded_size": sumDownloaded,
			"child_total": len(children), "child_failed": failed,
		})
	}
}

// pluralFailed 父任务终态文案（如实标注失败子数）。
func pluralFailed(failed, total int) string {
	if failed == 0 {
		return "批量任务全部完成（" + itoa(total) + " 项）"
	}
	return "批量任务结束：" + itoa(total) + " 项中 " + itoa(failed) + " 项失败，可单独重试失败项"
}

// itoa 免 strconv 依赖的小工具。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
