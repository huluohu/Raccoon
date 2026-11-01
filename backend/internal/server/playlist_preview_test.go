package server

// playlist_preview_test.go：解析预览与下载的口径一致性（接口级）。
//
// 2026-10-30 定位：「预览说 1 个视频、下载下 25 个」——预览用
// --playlist-items 1 只解析第一条（快），但下载阶段会拉**整个列表**。
// 修复是让预览如实报出条目数（playlist_count），界面据此告知。
//
// 这里用假 yt-dlp（httptest 同款脚本）驱动真实 HTTP 接口，验证
// 「引擎吐出来的条目数 → store.ParsePreview → API 响应」整条链路。

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withFakeYTDLPPayload 装一个按指定 JSON 输出的假 yt-dlp。
func withFakeYTDLPPayload(t *testing.T, dumpJSON string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "yt-dlp")
	body := fmt.Sprintf(`#!/bin/sh
case " $* " in
  *" --version "*) echo "2026.01.01-fake"; exit 0;;
  *" --dump-single-json "*) cat <<'JSONEOF'
%s
JSONEOF
    exit 0;;
esac
exit 0
`, dumpJSON)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

// Test解析预览_报出播放列表条目数 合集/播放列表链接的预览必须报出总条目数，
// 否则用户看到「1 个视频」却下载了 25 个。
func Test解析预览_报出播放列表条目数(t *testing.T) {
	withFakeYTDLPPayload(t, `{
  "id": "PL123",
  "title": "合集标题",
  "uploader": "u",
  "duration": 100,
  "extractor_key": "BiliBili",
  "extractor": "bilibili",
  "_type": "playlist",
  "playlist_count": 25,
  "formats": [
    {"format_id":"v1080","height":1080,"vcodec":"avc1.640032","acodec":"none","url":"https://cdn.example/v.m4s","filesize_approx":69793285,"tbr":2629}
  ],
  "entries": [{"id":"e1","title":"第一条","duration":100,"url":"https://x/1","formats":[]}]
}`)
	_, baseURL, user, pass := newSecEnv(t)
	cookie, csrf := secLoginSession(t, baseURL, user, pass)

	code, raw, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/parse",
		`{"url":"https://www.bilibili.com/video/BV1xx411c7mD"}`, secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("解析应 200，实际 %d（%s）", code, raw)
	}
	if !strings.Contains(raw, `"playlist_count":25`) {
		t.Errorf("预览响应应含 playlist_count=25，实际 %s", raw)
	}
}

// Test解析预览_单个视频不报条目数 单个视频没有列表概念，不该报条目数。
func Test解析预览_单个视频不报条目数(t *testing.T) {
	withFakeYTDLPPayload(t, `{
  "id": "BV1",
  "title": "单个视频",
  "uploader": "u",
  "duration": 10,
  "extractor_key": "BiliBili",
  "extractor": "bilibili",
  "_type": "video",
  "formats": [
    {"format_id":"v","height":1080,"vcodec":"avc1.640032","acodec":"none","url":"https://cdn.example/v.m4s","filesize_approx":69793285,"tbr":2629},
    {"format_id":"a","height":0,"vcodec":"none","acodec":"mp4a.40.2","url":"https://cdn.example/a.m4s","filesize_approx":5410339,"tbr":204}
  ]
}`)
	_, baseURL, user, pass := newSecEnv(t)
	cookie, csrf := secLoginSession(t, baseURL, user, pass)

	code, raw, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/parse",
		`{"url":"https://www.bilibili.com/video/BV1xx411c7mD"}`, secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("解析应 200，实际 %d（%s）", code, raw)
	}
	if strings.Contains(raw, `"playlist_count"`) {
		t.Errorf("单个视频不应报 playlist_count，实际 %s", raw)
	}
	// 体积应是成品口径（视频 69793285 + 音频 5410339 = 75203624）。
	if !strings.Contains(raw, "75203624") {
		t.Errorf("预览体积应为成品口径（视频+音轨=75203624），实际 %s", raw)
	}
}
