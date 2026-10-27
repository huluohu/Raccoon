package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/store"
)

// Test抽封面_音频当场拒绝 音频没有画面可抽：ffmpeg -frames:v 必然一帧都取不到，
// 与其让它跑完 6 次再报「未能抽取任何画面」，不如在入口就说清楚。
func Test抽封面_音频当场拒绝(t *testing.T) {
	srv, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	m, _ := newTestMediaFile(t, srv, "sample.mp3")
	// 把这条记录改成音频（helper 默认登记的是 video）
	if err := srv.store.UpsertMediaFile(&store.MediaFile{
		ID: m.ID, Path: m.Path, Name: m.Name, Size: m.Size, Kind: "audio",
	}); err != nil {
		t.Fatalf("登记音频文件失败：%v", err)
	}

	code, body, _ := secDoBody(t, http.MethodPost, base+"/api/v1/files/"+m.ID+"/cover/candidates",
		"", secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusBadRequest {
		t.Fatalf("音频抽封面应返回 400，实际 %d (body: %s)", code, body)
	}
	if !strings.Contains(body, "视频") {
		t.Fatalf("错误应说明只有视频能抽帧，实际: %s", body)
	}
}
