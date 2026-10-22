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
// 判据：yt-dlp --list-impersonate-targets 能正常执行且输出里含 curl_cffi。
// 该命令在缺少 curl_cffi 时会打印警告并以非零退出，且不列出任何 target。
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
func probeCurlCffi(bin string) bool {
	out, err := exec.CommandContext(context.Background(), bin, "--list-impersonate-targets").Output()
	if err != nil {
		// 该命令在无 curl_cffi 时以非零码退出，属预期情况，不算错误。
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "curl_cffi")
}
