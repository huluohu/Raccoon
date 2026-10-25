package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/store"
)

func TestConvertFile_InvalidTarget(t *testing.T) {
	srv, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)
	m, _ := newTestMediaFile(t, srv, "sample.mp4")

	code, body, _ := secDoBody(t, http.MethodPost, base+"/api/v1/files/"+m.ID+"/convert",
		`{"target":"flv"}`, secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusBadRequest {
		t.Fatalf("不支持的目标格式应返回 400，实际 %d (body: %s)", code, body)
	}
	if !strings.Contains(body, "不支持") {
		t.Fatalf("错误应提示不支持，实际: %s", body)
	}
}

func TestConvertFile_NotFound(t *testing.T) {
	_, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	code, _, _ := secDoBody(t, http.MethodPost, base+"/api/v1/files/nonexistent/convert",
		`{"target":"mp3"}`, secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusNotFound {
		t.Fatalf("文件不存在应返回 404，实际 %d", code)
	}
}

func TestConvertFile_RealAudioExtraction(t *testing.T) {
	ffmpeg := engine.Lookup("ffmpeg")
	if ffmpeg == "" {
		t.Skip("本地无 ffmpeg，跳过真实音频提取测试")
	}

	srv, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	// 用 ffmpeg 产生一个真实的 1s 视频带音频
	dataDir := srv.cfg.GetString("storage.data_dir")
	videoPath := filepath.Join(dataDir, "downloads", "test_video.mp4")
	_ = os.MkdirAll(filepath.Dir(videoPath), 0o755)
	err := engine.RunCmd(context.Background(), ffmpeg, []string{
		"-y", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=1",
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono",
		"-t", "1", "-c:v", "libx264", "-c:a", "aac", videoPath,
	}, nil, "")
	if err != nil {
		t.Skipf("生成测试视频失败: %v", err)
	}

	m := &store.MediaFile{
		ID:   "mf_test_video",
		Path: videoPath,
		Name: "test_video.mp4",
		Size: 1024,
		Kind: "video",
	}
	_ = srv.store.UpsertMediaFile(m)

	code, body, _ := secDoBody(t, http.MethodPost, base+"/api/v1/files/"+m.ID+"/convert",
		`{"target":"mp3"}`, secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("提取音频应返回 200，实际 %d (body: %s)", code, body)
	}
	if !strings.Contains(body, `"ok":true`) || !strings.Contains(body, "test_video.mp3") {
		t.Fatalf("响应应包含新提取的 mp3 文件信息，实际: %s", body)
	}

	// 确认数据库里出现了新的音频记录
	items, _, err := srv.store.ListMediaFiles(store.FileFilter{Kind: "audio"})
	if err != nil || len(items) == 0 {
		t.Fatalf("媒体库中应出现转换后的音频记录，实际: %v", items)
	}
	if items[0].Kind != "audio" || !strings.HasSuffix(items[0].Name, ".mp3") {
		t.Fatalf("新条目类型应为 audio，得到 %+v", items[0])
	}
}
