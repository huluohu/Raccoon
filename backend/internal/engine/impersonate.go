package engine

import (
	"context"
	"os/exec"
	"strings"
	"sync"
)

// curl_cffi 探测缓存。
//
// 探测代价是一次子进程启动（yt-dlp 是 Python 启动，约 0.3–0.5s），
// 而每次解析/下载都要用这个判断，必须缓存——但不能永久缓存：
// 引擎可能在运行期被替换/升级，缓存会失真。这里用 sync.Once 缓存进程内结果，
// 进程重启即重新探测（部署升级本来就要重启，语义一致）。
var (
	curlCffiOnce sync.Once
	curlCffiHas  bool

	// curlCffiProbe 探测实现，测试可替换。
	curlCffiProbe = probeCurlCffi
)

// hasCurlCffi 检测当前 yt-dlp 运行时是否具备 TLS 指纹伪装能力。
//
// 判据：yt-dlp --list-impersonate-targets 能正常执行，且输出里存在一个
// **真正可用**的 target。
//
// 为什么不直接探测 python 的 import：yt-dlp 可能不是 Python 装的（standalone
// 二进制内嵌解释器），系统 python 里有没有 curl_cffi 与它能否用是两回事。
// 用 yt-dlp 自己的命令探测最准。
func hasCurlCffi(bin string) bool {
	curlCffiOnce.Do(func() {
		if bin == "" {
			return
		}
		curlCffiHas = curlCffiProbe(bin)
	})
	return curlCffiHas
}

// probeCurlCffi 实际探测：执行 yt-dlp --list-impersonate-targets 并检查输出。
//
// 判据的坑（2026-10-29 实测）：**退出码不能用来判可用性**。
// 缺 curl_cffi 时，yt-dlp 仍以退出码 0 结束，并把每个 target 都列出来，
// 只是来源列标着 "curl_cffi (unavailable)"：
//
//	[info] Available impersonate targets
//	Client    OS   Source
//	--------------------------------------
//	Tor       -    curl_cffi>=0.11 (unavailable)
//	Edge      -    curl_cffi (unavailable)
//
// 早先只判「输出里含 curl_cffi」，于是这种「列得出来但一个都用不了」的
// 情况被误判成具备能力 → 引擎给**所有**请求加上 --impersonate chrome
// → yt-dlp 直接报错退出（Impersonate target "chrome" is not available）
// → 解析与下载全军覆没，且报错与「站点要求伪装」毫无关系，极难归因。
//
// 正确判据：输出里存在至少一个**不带 (unavailable) 标记**的 target 行。
func probeCurlCffi(bin string) bool {
	out, err := exec.CommandContext(context.Background(), bin, "--list-impersonate-targets").Output()
	if err != nil {
		// 该命令在无 curl_cffi 时以非零码退出，属预期情况，不算错误。
		return false
	}
	return hasUsableImpersonateTarget(string(out))
}

// hasUsableImpersonateTarget 判断 --list-impersonate-targets 的输出里
// 是否存在至少一个**可用**的伪装 target。
//
// 输出形如（每行一个 target，"Source" 列标明来源）：
//
//	Chrome-133      Macos-15     curl_cffi
//	Tor             Macos-14     curl_cffi>=0.11 (unavailable)
//
// 规则：
//   - 只看 target 行（Source 列之前有 target 名的行），跳过表头；
//   - 该行出现 "(unavailable)" 即视为不可用；
//   - 出现任意一行可用 → 具备伪装能力。
//
// 找不到可用行时返回 false（宁可「不伪装」，也不要让 --impersonate
// 把所有请求打成失败）。
func hasUsableImpersonateTarget(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		lower := strings.ToLower(l)
		// 表头与分隔线：没有 source 列的内容不算 target 行。
		// "source" 是表头词，但它本身不是 target 行的来源列值，
		// 这里按「含 unavailable 标记」判不可用，其余含 ':' 的行才算 target。
		if !strings.Contains(lower, "curl_cffi") {
			continue
		}
		if strings.Contains(lower, "unavailable") {
			continue // 明确不可用
		}
		// 形如 "Chrome-133  Macos-15  curl_cffi" —— 有来源且未标不可用。
		return true
	}
	return false
}
