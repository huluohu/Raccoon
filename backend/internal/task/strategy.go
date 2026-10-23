package task

// strategy.go：存储策略路由（docs/01 §3.4「本地下载 / 115 优先 / 失败转 qB」，
// 语义定本见 docs/15 §3.7）。
//
// 三值策略只对**磁力/种子/离线类任务**生效（Submit 时 kind=KindQB 或显式 KindN115）；
// 普通直链任务永远本地下载，不受策略影响。
//
//	| 策略值        | Submit 时路由              | 运行期转移 |
//	|--------------|---------------------------|-----------|
//	| local（默认） | magnet → qB；n115 仅显式指定  | 无 |
//	| n115_first   | magnet/种子 → 115 离线提交    | 115 确定失败 → 自动转 qB；结果不确定先查询不转移 |
//	| qb_fallback  | magnet/种子 → 直接 qB       | 无 |
//
// 默认值取 local：存量用户升级后行为零变化，策略显式可选。
// 任务详情「走了哪条路」由 tasks.route_path 承载（人类可读链路，随执行追补）。

import "strings"

// 存储策略取值（与 config.Registry 的 storage.strategy 一致）。
const (
	StrategyLocal      = "local"
	StrategyN115First  = "n115_first"
	StrategyQBFallback = "qb_fallback"
)

// NormalizeStrategy 归一化策略值；非法值回落 local（默认，零行为变化）。
func NormalizeStrategy(v string) string {
	switch v {
	case StrategyN115First:
		return StrategyN115First
	case StrategyQBFallback:
		return StrategyQBFallback
	default:
		return StrategyLocal
	}
}

// SubmitKindFor 按策略决定磁力/种子任务的提交类型。
// 显式指定 kind 的调用方优先（manager.Submit 已保证 it.Kind != "" 时不走这里）。
func SubmitKindFor(strategy string) string {
	if NormalizeStrategy(strategy) == StrategyN115First {
		return KindN115
	}
	return KindQB
}

// InitialRoutePath 任务创建时的「走了哪条路」初始值。
func InitialRoutePath(kind, strategy string) string {
	switch kind {
	case KindQB:
		return "qB"
	case KindN115:
		if NormalizeStrategy(strategy) == StrategyN115First {
			return "115 优先 → 115 提交中"
		}
		return "115 离线提交"
	default:
		return "本地下载"
	}
}

// AppendRoutePath 在链路尾部追补一段（空链路直接返回段本身）。
func AppendRoutePath(current, seg string) string {
	if current == "" {
		return seg
	}
	return current + " → " + seg
}

// IsDefiniteN115Failure 判断 115 失败是否为「确定失败」——只有确定失败才允许
// 自动转 qB（docs/15 §3.7：n115 侧未产生任何远程副作用时才转移；结果不确定
// 先查询，不转移，避免两边重复下载）。
//
// 确定失败：未配置 115 连接（nil 客户端）、提交被拒（提交-查询闭环的提交侧错误）。
// 不确定失败：轮询期查询错误（可能只是网络抖动，重试到 max_attempts 再判）。
func IsDefiniteN115Failure(code string) bool {
	switch code {
	case "N115_UNAVAILABLE", "N115_SUBMIT_FAILED", "N115_FAILED":
		return true
	default:
		return false
	}
}

// ShouldFailoverToQB 运行期转移判定：策略=n115_first 且 115 确定失败 → 转 qB。
// attempt 不递增——这是路由转移不是重试（docs/15 §3.7）。
//
// 已知边界（如实记录）：策略在任务创建后本应被 config_version 固化，但配置中心
// 只存当前值；此处读运行期策略。若用户在中途把策略从 n115_first 改走，
// 进行中的 115 任务按新策略判转移——route_path 已记录创建时的链路，不影响可解释性。
func ShouldFailoverToQB(strategy, n115ErrorCode string) bool {
	return NormalizeStrategy(strategy) == StrategyN115First && IsDefiniteN115Failure(n115ErrorCode)
}

// RoutePathIsN115First 该任务的创建链路是否为「115 优先」（用于界面/测试断言）。
func RoutePathIsN115First(routePath string) bool {
	return strings.HasPrefix(routePath, "115 优先")
}
