package engine

// quality_test.go：免登录下载的画质选择与格式探测。
//
// 锁死的不变式（2026-10-29 实测，B 站匿名访问）：
//
//	1) 同分辨率下必须取**码率最高**的那条流。yt-dlp 默认排序把 vcodec
//	   排在 br 之前且偏好 AV1，实测 1080p 选中 AV1（604kbps/21MB），
//	   而 AVC（2629kbps/75MB）被跳过 —— 同为 1080P，画质差 4 倍。
//	2) 解析预览展示的体积必须与下载产物一致（早先预览取最小体积、
//	   下载取最大码率，界面在说谎）。
//	3) 缺 curl_cffi 时**绝不能**加 --impersonate（否则全部站点全挂）。

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// f64 取 float64 指针（构造测试用的码率字段）。
func f64(v float64) *float64 { return &v }

// i64 取 int64 指针（构造测试用的体积字段）。
func i64(v int64) *int64 { return &v }

// ---- 格式排序参数 ----

// TestFormatSortArgs_存在且为站点无关的排序字段 排序必须用 res/fps/br 这类
// 通用字段，不能写成 [vcodec^=avc1] 那种只对部分站点有意义的表达式。
func TestFormatSortArgs_存在且为站点无关的排序字段(t *testing.T) {
	args := formatSortArgs()
	if len(args) != 2 || args[0] != "--format-sort" {
		t.Fatalf("应产出 --format-sort <字段>，实际 %v", args)
	}
	sort := args[1]
	for _, want := range []string{"res", "fps", "br"} {
		if !strings.Contains(sort, want) {
			t.Errorf("排序字段应含 %q（分辨率/帧率/码率），实际 %q", want, sort)
		}
	}
	// br 必须排在 vcodec 之前才有意义——但 vcodec 不该出现在排序里，
	// 否则又会退回「偏好 AV1」的老坑。
	if strings.Contains(sort, "vcodec") {
		t.Errorf("排序不应含 vcodec（那正是默认排序偏好 AV1 的原因），实际 %q", sort)
	}
}

// TestFormatSortArgs_码率排在分辨率之后 码率只在同分辨率内比较，
// 不能让它盖过分辨率（否则 360p 高码率会赢过 1080p）。
func TestFormatSortArgs_码率排在分辨率之后(t *testing.T) {
	sort := formatSortArgs()[1]
	parts := strings.Split(sort, ",")
	resIdx, brIdx := -1, -1
	for i, p := range parts {
		switch p {
		case "res":
			resIdx = i
		case "br":
			brIdx = i
		}
	}
	if resIdx < 0 || brIdx < 0 {
		t.Fatalf("排序字段应同时含 res 与 br，实际 %q", sort)
	}
	if resIdx > brIdx {
		t.Errorf("res 应排在 br 之前（先分辨率后码率），实际 %q", sort)
	}
}

// ---- collectFormats：同分辨率取码率最高 ----

func TestCollectFormats_同分辨率取码率最高(t *testing.T) {
	// B 站 1080p 的三条流（真实数据）：同分辨率，码率差 4 倍。
	fs := []ytdlpFormat{
		{FormatID: "30080", Height: 1080, VCodec: "avc1.640032", FilesizeApprox: i64(69793285), TBR: f64(2629.0)},
		{FormatID: "100113", Height: 1080, VCodec: "hev1.1.6.L150", FilesizeApprox: i64(19892265), TBR: f64(749.0)},
		{FormatID: "100026", Height: 1080, VCodec: "av01.0.08M.08", FilesizeApprox: i64(16020000), TBR: f64(604.0)},
	}
	got := collectFormats(fs)
	if len(got) != 1 {
		t.Fatalf("同分辨率应只展示一档，得到 %d 档：%+v", len(got), got)
	}
	// 期望选中 AVC（码率最高），不是 AV1。
	if got[0].Size == nil || *got[0].Size != 69793285 {
		t.Fatalf("应选码率最高的 AVC 流（69793285 字节），实际 size=%v", got[0].Size)
	}
}

// TestCollectFormats_无码率时退回比体积 有的站点不报 tbr，
// 此时用体积兜底，仍应取大的那条（画质更好）。
func TestCollectFormats_无码率时退回比体积(t *testing.T) {
	fs := []ytdlpFormat{
		{FormatID: "small", Height: 720, VCodec: "avc1.640028", FilesizeApprox: i64(1000)},
		{FormatID: "big", Height: 720, VCodec: "avc1.640028", FilesizeApprox: i64(9000)},
	}
	got := collectFormats(fs)
	if len(got) != 1 || got[0].Size == nil || *got[0].Size != 9000 {
		t.Fatalf("无码率时应取体积大的那条（9000），实际 %+v", got)
	}
}

// TestCollectFormats_缺体积信息时保留先到的一条 站点没给体积也没给码率时，
// 不应把先到的候选丢掉（否则预览会凭空少一档）。
func TestCollectFormats_缺体积信息时保留先到的一条(t *testing.T) {
	fs := []ytdlpFormat{
		{FormatID: "first", Height: 1080, VCodec: "avc1.640032"},
		{FormatID: "second", Height: 1080, VCodec: "avc1.640032"},
	}
	got := collectFormats(fs)
	if len(got) != 1 {
		t.Fatalf("应保留一档，得到 %d 档", len(got))
	}
	if got[0].Size != nil {
		t.Fatalf("没有体积信息时 size 应为空，实际 %v", *got[0].Size)
	}
}

// TestCollectFormats_各分辨率各自取最优 每个分辨率独立成档，
// 1080p 取 AVC、720p 取该档码率最高的，不能跨分辨率串味。
func TestCollectFormats_各分辨率各自取最优(t *testing.T) {
	fs := []ytdlpFormat{
		{FormatID: "hd_avc", Height: 1080, VCodec: "avc1.640032", FilesizeApprox: i64(70000), TBR: f64(2600.0)},
		{FormatID: "hd_av1", Height: 1080, VCodec: "av01.0.08M.08", FilesizeApprox: i64(16000), TBR: f64(600.0)},
		{FormatID: "sd_av1", Height: 480, VCodec: "av01.0.08M.08", FilesizeApprox: i64(5000), TBR: f64(300.0)},
		{FormatID: "sd_avc", Height: 480, VCodec: "avc1.64001F", FilesizeApprox: i64(20000), TBR: f64(800.0)},
	}
	got := collectFormats(fs)
	if len(got) != 2 {
		t.Fatalf("两个分辨率应出两档，得到 %d：%+v", len(got), got)
	}
	if got[0].Quality != "1080p" || got[1].Quality != "480p" {
		t.Fatalf("分辨率应由高到低排列，实际 %s / %s", got[0].Quality, got[1].Quality)
	}
	if got[0].Size == nil || *got[0].Size != 70000 {
		t.Errorf("1080p 应取码率最高的 AVC（70000），实际 %v", got[0].Size)
	}
	if got[1].Size == nil || *got[1].Size != 20000 {
		t.Errorf("480p 应取该档码率最高的 AVC（20000），实际 %v", got[1].Size)
	}
}

// TestCollectFormats_音频档取码率最高 音频同理：bestaudio 的口径是码率最高，
// 早先取最小体积会让预览显示最小的音轨。
func TestCollectFormats_音频档取码率最高(t *testing.T) {
	fs := []ytdlpFormat{
		{FormatID: "a_low", VCodec: "none", ACodec: "mp4a.40.5", FilesizeApprox: i64(1000000), TBR: f64(44.0)},
		{FormatID: "a_high", VCodec: "none", ACodec: "mp4a.40.2", FilesizeApprox: i64(5410339), TBR: f64(204.0)},
	}
	got := collectFormats(fs)
	if len(got) != 1 {
		t.Fatalf("应出 1 个音频档，得到 %d", len(got))
	}
	if got[0].Quality != "audio" {
		t.Fatalf("音频档 quality 应为 audio，实际 %q", got[0].Quality)
	}
	if got[0].Size == nil || *got[0].Size != 5410339 {
		t.Errorf("应取码率最高的音轨（5410339），实际 %v", got[0].Size)
	}
}

// ---- 伪装能力探测 ----

// TestHasUsableImpersonateTarget_全unavailable时判定为不可用
//
// 真实 yt-dlp 输出（缺 curl_cffi 的独立二进制）：**退出码仍是 0**，
// 且每个 target 的 Source 列都标着 (unavailable)。
// 早先只判「输出含 curl_cffi」→ 误判为具备能力 → 给所有请求加
// --impersonate chrome → yt-dlp 直接报错退出 → 全站解析/下载失败。
func TestHasUsableImpersonateTarget_全unavailable时判定为不可用(t *testing.T) {
	out := `[info] Available impersonate targets
Client    OS   Source
--------------------------------------------
Tor       -    curl_cffi>=0.11 (unavailable)
Edge      -    curl_cffi (unavailable)
`
	if hasUsableImpersonateTarget(out) {
		t.Fatal("全部 target 都标着 unavailable 时应判定为不具备伪装能力")
	}
}

// TestHasUsableImpersonateTarget_有可用target时判定为可用
func TestHasUsableImpersonateTarget_有可用target时判定为可用(t *testing.T) {
	out := `[info] Available impersonate targets
Client          OS   Source
--------------------------------------------
Chrome-133      Macos-15     curl_cffi
Safari-18.0     Ios-18.0     curl_cffi
`
	if !hasUsableImpersonateTarget(out) {
		t.Fatal("存在可用 target 时应判定为具备伪装能力")
	}
}

// TestHasUsableImpersonateTarget_混合时以可用target为准 部分可用即算可用
// （比如 curl_cffi 装了但某个 target 的版本要求没满足）。
func TestHasUsableImpersonateTarget_混合时以可用target为准(t *testing.T) {
	out := `[info] Available impersonate targets
Client    OS   Source
--------------------------------------------
Tor       -    curl_cffi>=0.11 (unavailable)
Chrome    Macos-15     curl_cffi
`
	if !hasUsableImpersonateTarget(out) {
		t.Fatal("存在任一可用 target 即应判定为可用")
	}
}

// TestHasUsableImpersonateTarget_空输出判定为不可用
func TestHasUsableImpersonateTarget_空输出判定为不可用(t *testing.T) {
	if hasUsableImpersonateTarget("") {
		t.Fatal("空输出应判定为不具备伪装能力")
	}
}

// TestProbeCurlCffi_不可用时不得启用伪装参数 端到端守住那条铁律：
// 缺 curl_cffi 时**绝不能**加 --impersonate，否则 yt-dlp 直接报错退出，
// 所有站点一起不可用（不只是需要伪装的那个）。
func TestProbeCurlCffi_不可用时不得启用伪装参数(t *testing.T) {
	orig := curlCffiProbe
	t.Cleanup(func() {
		curlCffiProbe = orig
		curlCffiOnce = sync.Once{}
		curlCffiHas = false
	})

	curlCffiOnce = sync.Once{}
	curlCffiProbe = func(bin string) bool {
		return hasUsableImpersonateTarget(`Tor       -    curl_cffi>=0.11 (unavailable)`)
	}
	y := NewYTDLP("/usr/local/bin/yt-dlp", "")
	if got := y.impersonateArgs(); len(got) != 0 {
		t.Fatalf("探测为不可用时不应加伪装参数，实际 %v", got)
	}
}

// ---- JS 运行时 ----

// TestJsRuntimeArgs_无可用运行时时不加参数 没有 JS 运行时时**不能**加
// --js-runtimes（否则 yt-dlp 会警告「Ignoring unsupported runtime」）。
// 此时 yt-dlp 走 jsless 退化路径，但不会失败（实测 exit=0）。
func TestJsRuntimeArgs_无可用运行时时不加参数(t *testing.T) {
	orig := jsRuntimeProbe
	t.Cleanup(func() {
		jsRuntimeProbe = orig
		jsRuntimeOnce = sync.Once{}
		jsRuntimeName = ""
	})

	jsRuntimeOnce = sync.Once{}
	jsRuntimeProbe = func() string { return "" }
	if got := jsRuntimeArgs(); len(got) != 0 {
		t.Fatalf("无可用运行时时不应加参数，实际 %v", got)
	}
}

// TestJsRuntimeArgs_有运行时时传对应的名字 必须传 yt-dlp 认识的运行时名
// （deno / node / quickjs / bun），quickjs 对应可执行文件 qjs。
func TestJsRuntimeArgs_有运行时时传对应的名字(t *testing.T) {
	orig := jsRuntimeProbe
	t.Cleanup(func() {
		jsRuntimeProbe = orig
		jsRuntimeOnce = sync.Once{}
		jsRuntimeName = ""
	})

	jsRuntimeOnce = sync.Once{}
	jsRuntimeProbe = func() string { return "quickjs" }
	got := jsRuntimeArgs()
	if len(got) != 2 || got[0] != "--js-runtimes" || got[1] != "quickjs" {
		t.Fatalf("应产出 --js-runtimes quickjs，实际 %v", got)
	}
}

// TestJsRuntimeArgs_一个flag一个运行时名 逗号分隔会被 yt-dlp 当成一个非法
// 名字而整组丢弃（实测 "Ignoring unsupported JavaScript runtime(s):
// deno,quickjs"，两个都不生效）。运行时名里不得含逗号。
func TestJsRuntimeArgs_一个flag一个运行时名(t *testing.T) {
	for _, c := range jsRuntimeCandidates {
		if strings.Contains(c.name, ",") {
			t.Errorf("运行时名 %q 不应含逗号（yt-dlp 不认逗号分隔）", c.name)
		}
		if strings.Contains(c.bin, ",") {
			t.Errorf("可执行文件名 %q 不应含逗号", c.bin)
		}
	}
}

// TestJsRuntimeCandidates_含quickjs 镜像装的是 quickjs-ng（可执行文件 qjs），
// 候选表必须覆盖它，否则免登录画质这条链路在镜像里根本不会生效。
func TestJsRuntimeCandidates_含quickjs(t *testing.T) {
	var found bool
	for _, c := range jsRuntimeCandidates {
		if c.name == "quickjs" && c.bin == "qjs" {
			found = true
		}
	}
	if !found {
		t.Fatalf("候选表应含 quickjs/qjs，实际 %+v", jsRuntimeCandidates)
	}
}

// ---- 分辨率档位（4K/2K/8K）------

// TestFormatSelector_任意分辨率档位都走高度过滤
//
// 2026-10-30 修的真 bug：预览下拉的档位来自 collectFormats 的
// `fmt.Sprintf("%dp", height)`——平台给什么高度就出现什么档位。旧代码只硬编码
// 360p/480p/720p/1080p，4K（2160p）、2K（1440p）、8K（4320p）落到 default
// 分支被**当 format id 透传**，yt-dlp 收到 `-f 2160p` 找不到该 id 就报
// "Requested format is not available" —— 用户在下拉里选 4K，任务必失败。
func TestFormatSelector_任意分辨率档位都走高度过滤(t *testing.T) {
	cases := []struct {
		q     string
		wantH int
	}{
		{"360p", 360},
		{"480p", 480},
		{"720p", 720},
		{"1080p", 1080},
		{"1440p", 1440},
		{"2160p", 2160},
		{"4320p", 4320},
		{"1080p60", 1080},
	}
	for _, c := range cases {
		got := formatSelector(c.q)
		want := fmt.Sprintf("bv*[height<=%d]+ba/b[height<=%d]/b", c.wantH, c.wantH)
		if got != want {
			t.Errorf("formatSelector(%q) = %q，期望 %q", c.q, got, want)
		}
		// 关键：绝不能把档位原样透传（那是 format id 语义）。
		if got == c.q {
			t.Errorf("formatSelector(%q) 把档位当 format id 透传了，会导致任务失败", c.q)
		}
	}
}

// TestFormatSelector_真正的formatid仍透传 format id 不能被误当成分辨率：
// 数字 id（137）、带前缀 id（hls-383）、长数字 id（100026）都要原样透传。
func TestFormatSelector_真正的formatid仍透传(t *testing.T) {
	for _, id := range []string{"137", "hls-383", "100026", "616", "abc", "22"} {
		if got := formatSelector(id); got != id {
			t.Errorf("formatSelector(%q) = %q，应原样透传", id, got)
		}
	}
}

// TestFormatSelector_特殊档位语义不变 best / audio 不受影响。
func TestFormatSelector_特殊档位语义不变(t *testing.T) {
	if got := formatSelector("best"); got != "bv*+ba/b" {
		t.Errorf("best 应为 bv*+ba/b，实际 %q", got)
	}
	if got := formatSelector(""); got != "bv*+ba/b" {
		t.Errorf("空值应按 best 处理，实际 %q", got)
	}
	if got := formatSelector("audio"); got != "bestaudio/best" {
		t.Errorf("audio 应为 bestaudio/best，实际 %q", got)
	}
}

// TestParseHeightQuality_识别与拒绝 覆盖解析函数的边界。
func TestParseHeightQuality_识别与拒绝(t *testing.T) {
	if h, ok := parseHeightQuality("2160p"); !ok || h != 2160 {
		t.Errorf("2160p 应解析为 2160，实际 %d ok=%v", h, ok)
	}
	if h, ok := parseHeightQuality("1080p60"); !ok || h != 1080 {
		t.Errorf("1080p60 应解析为 1080，实际 %d ok=%v", h, ok)
	}
	for _, bad := range []string{"137", "hls-383", "audio", "best", "p", "2160", "2160px"} {
		if _, ok := parseHeightQuality(bad); ok {
			t.Errorf("%q 不应被当作分辨率档位", bad)
		}
	}
}

// TestCollectFormats_4K档位出现在预览里 平台给 2160p 时，预览必须列出这一档，
// 用户才能选到它（选不到就谈不上 4K）。
func TestCollectFormats_4K档位出现在预览里(t *testing.T) {
	fs := []ytdlpFormat{
		{FormatID: "2160p_h264", Height: 2160, VCodec: "avc1.640033", FilesizeApprox: i64(400000000), TBR: f64(18000.0)},
		{FormatID: "1080p_h264", Height: 1080, VCodec: "avc1.640032", FilesizeApprox: i64(80000000), TBR: f64(6000.0)},
	}
	got := collectFormats(fs)
	if len(got) != 2 {
		t.Fatalf("应出两档，得到 %d：%+v", len(got), got)
	}
	if got[0].Quality != "2160p" {
		t.Errorf("最高档应为 2160p，实际 %q", got[0].Quality)
	}
	if got[0].Size == nil || *got[0].Size != 400000000 {
		t.Errorf("2160p 应取码率最高的那条（400000000），实际 %v", got[0].Size)
	}
}
