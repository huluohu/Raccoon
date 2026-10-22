package qb

// state.go：qB 原始状态词表的归一化，以及进度单位换算。
//
// 为什么要归一：qB 的状态串把"方向"和"阶段"编码进了同一个词（stalledDL =
// 停滞的下载、pausedUP = 暂停的做种），一共二十来个取值且随版本增补。
// 上层展示与落库都不该依赖这些词，所以在这里统一收敛成固定的状态常量。

import "strings"

// 归一后的种子状态。
const (
	// StateDownloading 正在下载（含下载方向的全部子状态）。
	StateDownloading = "downloading"
	// StateSeeding 做种中（已下完，暂无上传流量）。
	StateSeeding = "seeding"
	// StatePaused 已暂停（用户主动暂停）。
	StatePaused = "paused"
	// StateError 出错（下载失败、文件缺失等）。
	StateError = "error"
	// StateQueued 排队中（等待下载槽位）。
	StateQueued = "queued"
	// StateChecking 校验中（做完整性 hash）。
	StateChecking = "checking"
	// StateUploading 正在上传。
	StateUploading = "uploading"
	// StateStopped 已停止（既不下载也不上传）。
	StateStopped = "stopped"
	// StateCompleted 已完成下载。
	//
	// qB 没有这个状态：下载完成后种子会变成 stalledUP（空闲做种）或 uploading
	//（正在上传），只能靠 progress==1 判定"已完成"，见 NormalizeTorrentState。
	StateCompleted = "completed"
)

// NormalizeState 把 qB 原始状态串归一成上层状态常量。
//
// 映射依据 qB 4.x 的状态词表（未在真实实例实测）：
//   - 下载方向：downloading / stalledDL / forcedDL / metaDL / forcedMetaDL
//     / allocating / moving
//   - 上传方向：uploading / forcedUP（正在传）/ stalledUP（空闲做种）
//   - 阶段型：queuedUP / queuedDL、pausedUP / pausedDL、stoppedUP / stoppedDL、
//     checkingUP / checkingDL
//   - 异常型：error / missingFiles
//
// 认不出来的原始值一律归到 StateError：宁可报"状态异常"让人看一眼，也不要
// 静默落成一个正常状态（那会让"种子没动"这件事被当成"已完成"而漏报）。
func NormalizeState(raw string) string {
	switch strings.TrimSpace(raw) {
	case "downloading", "stalledDL", "forcedDL", "metaDL", "forcedMetaDL",
		"allocating", "moving":
		return StateDownloading
	case "uploading", "forcedUP":
		return StateUploading
	case "stalledUP":
		return StateSeeding
	case "pausedUP", "pausedDL", "pausedD":
		return StatePaused
	case "stoppedUP", "stoppedDL", "stoppedD":
		return StateStopped
	case "queuedUP", "queuedDL", "queuedD":
		return StateQueued
	case "checkingUP", "checkingDL":
		return StateChecking
	case "error", "missingFiles":
		return StateError
	default:
		return StateError
	}
}

// NormalizeTorrentState 在状态归一的基础上再判定"已完成"。
//
// qB 不下发"已完成"状态：种子下完后会变成 stalledUP（空闲做种），而
// stalledUP 与"下到一半"在状态串上完全一样，只能配合 progress 区分。
// 只有空闲做种（StateSeeding）才判完成——uploading 说明数据正在往上走，
// 报"上传中"比报"已完成"更有信息量。
func NormalizeTorrentState(raw string, progress float64) string {
	state := NormalizeState(raw)
	if state == StateSeeding && progress >= 1 {
		return StateCompleted
	}
	return state
}

// ProgressPercent 把 qB 的进度（0..1 小数）换算成百分比（0..100）。
//
// qB 的 progress 是 0..1，上层展示与数据库统一用 0..100，集中在这里换算，
// 免得各处各写一遍 ×100 而漏掉越界裁剪。
func ProgressPercent(raw float64) float64 {
	switch {
	case raw <= 0:
		return 0
	case raw >= 1:
		return 100
	default:
		return raw * 100
	}
}
