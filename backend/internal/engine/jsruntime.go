package engine

// jsruntime.go：JS 运行时探测与传参（免登录高画质的关键一环）。
//
// 为什么需要（2026-10-29 实测）：
//
// yt-dlp 解析 YouTube 靠 JS 解签名（nsig）。**没有 JS 运行时时，yt-dlp 会
// 退化成缩减版的 player client 列表**：
//
//	_DEFAULT_CLIENTS       = ('tv_samsung', 'visionos', 'web')   ← 有运行时
//	_DEFAULT_JSLESS_CLIENTS = ('visionos',)                       ← 无运行时
//
// 实测（容器内 yt-dlp --verbose）：
//
//	无运行时：[youtube] Downloading visionos player API JSON
//	有运行时：[youtube] Downloading tv samsung player API JSON
//	          [youtube] Downloading visionos player API JSON
//
// client 少了 → 可用格式池变小 → **免登录下载拿不到高画质**（tv_samsung
// 是匿名能出高画质的主力 client）。yt-dlp 自己也会警告：
// "YouTube extraction without a JS runtime has been deprecated, and some
// formats may be missing"。
//
// 所以「不登录也能下到好画质」这件事，JS 运行时是**硬前提**：
// 没有它，再怎么调格式选择也只是在残缺的格式池里挑。

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// jsRuntime 探测缓存。
//
// 与 curl_cffi 同样的理由：探测是一次子进程启动，而每次解析/下载都要用这个
// 判断，必须缓存；进程重启即重探（引擎升级本来就要重启）。
var (
	jsRuntimeOnce sync.Once
	jsRuntimeName string // 探测到的可用运行时名（"" = 没有）

	// jsRuntimeProbe 探测实现，测试可替换。
	jsRuntimeProbe = probeJSRuntime
)

// jsRuntimeCandidates 按优先级排列的候选运行时。
//
// 顺序与 yt-dlp 官方建议一致：deno > node > quickjs > bun。
// 我们的镜像装的是 quickjs-ng（2MB，deno 要 91MB），裸机部署若已装
// deno/node 则优先用它们。
var jsRuntimeCandidates = []struct {
	// name 是传给 yt-dlp --js-runtimes 的运行时名。
	name string
	// bin 是可执行文件名（yt-dlp 按这些名字在 PATH 里找）。
	bin string
}{
	{"deno", "deno"},
	{"node", "node"},
	{"quickjs", "qjs"},
	{"bun", "bun"},
}

// jsRuntimeArgs 返回应追加给 yt-dlp 的 --js-runtimes 参数组。
//
// 没有可用运行时时返回 nil —— 此时 yt-dlp 会走 jsless 退化路径，
// 但**不会报错**（实测 exit=0），只是格式池变小。
//
// 注意：--js-runtimes **一个 flag 一个运行时名**，逗号分隔会被当成
// 一个非法名字而整组被丢弃（yt-dlp 会警告
// "Ignoring unsupported JavaScript runtime(s): deno,quickjs"，
// 实测那样写会让两个运行时都不生效）。要多个就写多个 flag。
//
// 也不用覆盖 deno：镜像里装的是 quickjs，直接指定它即可；
// 裸机若已装 deno，探测会优先挑中 deno。
func jsRuntimeArgs() []string {
	jsRuntimeOnce.Do(func() {
		jsRuntimeName = jsRuntimeProbe()
	})
	if jsRuntimeName == "" {
		return nil
	}
	return []string{"--js-runtimes", jsRuntimeName}
}

// probeJSRuntime 探测本机可用的 JS 运行时，返回运行时名（没有则空串）。
func probeJSRuntime() string {
	for _, c := range jsRuntimeCandidates {
		if p, err := exec.LookPath(c.bin); err == nil && p != "" {
			return c.name
		}
	}
	return ""
}

// jsRuntimeAvailable 判断镜像/主机上是否具备 JS 运行时（诊断用）。
func jsRuntimeAvailable() bool {
	jsRuntimeOnce.Do(func() {
		jsRuntimeName = jsRuntimeProbe()
	})
	return jsRuntimeName != ""
}

// jsRuntimeBinPath 返回运行时可执行文件路径（诊断用；没有则空串）。
func jsRuntimeBinPath() string {
	for _, c := range jsRuntimeCandidates {
		if p, err := exec.LookPath(c.bin); err == nil && p != "" {
			return filepath.Base(p)
		}
	}
	return ""
}

// jsRuntimeExists 判断指定路径上是否存在可执行的 JS 运行时（供容器自检用）。
func jsRuntimeExists(dir, bin string) bool {
	p := filepath.Join(dir, bin)
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode().Perm()&0o111 != 0
}
