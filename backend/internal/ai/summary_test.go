package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/store"
)

func TestCleanSubtitleText(t *testing.T) {
	srt := `1
00:00:01,000 --> 00:00:04,000
欢迎观看本期精彩视频

2
00:00:05,000 --> 00:00:08,000
今天我们来介绍自托管下载中心
`
	got := cleanSubtitleText(srt)
	if strings.Contains(got, "-->") || strings.Contains(got, "00:00:01") {
		t.Fatalf("不应包含时间轴，实际：%q", got)
	}
	if !strings.Contains(got, "欢迎观看本期精彩视频") || !strings.Contains(got, "今天我们来介绍自托管下载中心") {
		t.Fatalf("应保留字幕文本内容，实际：%q", got)
	}
}

func TestCallChatCompletion_Success(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "### 视频总结\n1. 核心概述：自托管下载\n2. 重点看点：可靠性高",
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	summary, err := callChatCompletion(context.Background(), mockServer.URL, "test-key", "test-model", "sys", "content")
	if err != nil {
		t.Fatalf("调用应成功，得到错误：%v", err)
	}
	if !strings.Contains(summary, "核心概述") {
		t.Fatalf("应包含返回的总结内容，实际：%s", summary)
	}
}

func TestGenerateMediaSummary_Persistence(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "精炼总结：这是一段视频",
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := config.New()
	_ = cfg.Set("ai.endpoint", mockServer.URL)
	_ = cfg.Set("ai.api_key", "sk-test")

	// 写测试文件
	videoFile := filepath.Join(dir, "video.mp4")
	_ = os.WriteFile(videoFile, []byte("fake"), 0o644)
	descFile := filepath.Join(dir, "video.description")
	_ = os.WriteFile(descFile, []byte("这是视频的描述信息"), 0o644)

	m := &store.MediaFile{
		ID:   "mf_test",
		Path: videoPathClean(videoFile),
		Name: "video.mp4",
		Size: 100,
		Kind: "video",
	}
	_ = st.UpsertMediaFile(m)

	summarizer := NewSummarizer(cfg, st)
	summary, err := summarizer.GenerateMediaSummary(context.Background(), m)
	if err != nil {
		t.Fatalf("生成总结失败: %v", err)
	}
	if summary != "精炼总结：这是一段视频" {
		t.Fatalf("总结内容不匹配，得到: %s", summary)
	}

	// 验证已持久化至 store
	stored, err := st.GetMediaFile("mf_test")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Summary != "精炼总结：这是一段视频" {
		t.Fatalf("DB 中持久化的总结不匹配，得到: %q", stored.Summary)
	}
}

func videoPathClean(p string) string {
	return filepath.Clean(p)
}
