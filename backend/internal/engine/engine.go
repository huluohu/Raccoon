package engine

// engine.go：引擎装配与启动自检（docs/08 W1.6）。
//
// 自检语义：yt-dlp 必须可用（缺失 → ENGINE_UNAVAILABLE 503，/health 降级）；
// ffmpeg 缺失仅降级提示（不需合并的源仍可下载）。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Set 引擎集合（当前唯一下载引擎 yt-dlp；qb/n115 在 Wave 4 加入）。
type Set struct {
	YTDLP      *YTDLP
	FFmpegBin  string // 可为空（降级）
	ffmpegErr  error  // 缓存 ffmpeg 自检结果
	ffmpedDone bool
}

// NewSet 构造；路径为空时自动探测。
func NewSet(ytdlpBin, ffmpegBin string) *Set {
	if ytdlpBin == "" {
		ytdlpBin = Lookup("yt-dlp")
	}
	if ffmpegBin == "" {
		ffmpegBin = Lookup("ffmpeg")
	}
	return &Set{YTDLP: NewYTDLP(ytdlpBin, ffmpegBin), FFmpegBin: ffmpegBin}
}

// CheckAll 启动自检：yt-dlp 必须可用（失败即不可启动下载域）；
// ffmpeg 缺失只记录降级（不阻断）。
func (s *Set) CheckAll() error {
	return s.YTDLP.Available()
}

// EngineFor 按 URL 选引擎。
func (s *Set) EngineFor(rawURL string) Engine {
	return s.YTDLP
}

// PlatformGuess 从 URL 推断平台标识（创建任务时落库；解析成功后以 extractor 为准覆盖）。
func PlatformGuess(rawURL string) string { return platformOf("", rawURL) }

// Lookup 查找二进制：PATH > 常见安装位置（含用户 ~/bin，供开发机无 brew 环境）。
func Lookup(bin string) string {
	if p, err := exec.LookPath(bin); err == nil {
		return p
	}
	for _, dir := range []string{"/usr/local/bin", "/usr/bin", "/opt/homebrew/bin", "~/bin"} {
		p := expandHome(filepath.Join(dir, bin))
		if isRegularFile(p) {
			return p
		}
	}
	return bin // 兜底原名：exec 失败由 Available() 统一转 503
}

func isRegularFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}
