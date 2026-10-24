package server

// shortcut_flow_test.go：iOS 快捷指令契约的端到端验证（docs/12）。
//
// docs/12 是「契约即文件」的快捷指令搭建指引，但一直标注「未在真机实测」。
// 真机我们测不了，能做的是把它描述的每一步 HTTP 交互（签发 scope=3 令牌 →
// Bearer 提交任务（无 Cookie 无 CSRF）→ Bearer 查状态）在真路由上跑通——
// 契约在服务端侧从此有回归保障，快捷指令侧的动作组装照抄不会再踩接口变更。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/huluohu/raccoon/internal/store"
)

// withFakeYTDLPPATH 在 PATH 前置一个假 yt-dlp：--version 过自检、
// --dump-single-json 回最小元信息、其余调用静默退出。让「提交 → 查状态」
// 的契约链路在没有真引擎的开发机/CI 上也能回归（真引擎行为由容器验收覆盖）。
func withFakeYTDLPPATH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "yt-dlp")
	body := `#!/bin/sh
case " $* " in
  *" --version "*) echo "2026.01.01-fake"; exit 0;;
  *" --dump-single-json "*)
    echo '{"id":"fake","title":"契约测试","uploader":"u","duration":10,"thumbnail":"","extractor_key":"BiliBili","extractor":"bilibili","formats":[]}'
    exit 0;;
esac
exit 0
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

// TestShortcutContractFlow docs/12 §2 基线快捷指令：提交单个链接 + 查询状态。
func TestShortcutContractFlow(t *testing.T) {
	withFakeYTDLPPATH(t)
	_, baseURL, user, pass := newSecEnv(t)
	cookie, csrf := secLoginSession(t, baseURL, user, pass)

	// §1.2 在「设置」页签发读写令牌（scope=3）。
	_, token := secCreateToken(t, baseURL, cookie, csrf, "shortcut", store.ScopeRead|store.ScopeWrite)
	if token == "" {
		t.Fatal("令牌签发失败")
	}

	// §2.1 快捷指令的「获取 URL 内容」动作：POST /api/v1/tasks，
	// Authorization: Bearer（Token 通道豁免 CSRF，无会话 Cookie）。
	submitBody := fmt.Sprintf(
		`{"items":[{"url":"https://www.bilibili.com/video/BV1xx411c7mD","quality":"","save_path":""}]}`)
	code, raw, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/tasks", submitBody,
		secOpts{token: token})
	if code != http.StatusCreated {
		t.Fatalf("Bearer 提交应返回 201（引擎缺失除外），实际 %d（body: %s）", code, raw)
	}
	var created struct {
		Data []struct {
			ID     string `json:"id"`
			URL    string `json:"url"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &created); err != nil || len(created.Data) == 0 {
		t.Fatalf("创建响应应含任务数组: %v（%s）", err, raw)
	}
	taskID := created.Data[0].ID

	// §6 状态查询：GET /api/v1/tasks/{id}，同为 Bearer。
	code, raw, _ = secDoBody(t, http.MethodGet, baseURL+"/api/v1/tasks/"+taskID, "",
		secOpts{token: token})
	if code != http.StatusOK {
		t.Fatalf("Bearer 查询应返回 200，实际 %d（body: %s）", code, raw)
	}
	var detail struct {
		Data struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &detail); err != nil || detail.Data.ID != taskID {
		t.Fatalf("任务详情不符: %s", raw)
	}

	// 只读令牌（scope=1）必须被拒（docs/12 §1.2：写操作需 write 位）。
	_, roToken := secCreateToken(t, baseURL, cookie, csrf, "shortcut-ro", store.ScopeRead)
	code, _, _ = secDoBody(t, http.MethodPost, baseURL+"/api/v1/tasks", submitBody,
		secOpts{token: roToken})
	if code != http.StatusForbidden {
		t.Fatalf("只读令牌提交应返回 403，实际 %d", code)
	}
}

// TestShortcutContractParse docs/12 §4 变体：解析预览走 Bearer。
// 用假引擎（不联网）：契约验证的是通道语义，不是站点可达性。
func TestShortcutContractParse(t *testing.T) {
	withFakeYTDLPPATH(t)
	_, baseURL, user, pass := newSecEnv(t)
	cookie, csrf := secLoginSession(t, baseURL, user, pass)
	_, token := secCreateToken(t, baseURL, cookie, csrf, "shortcut-parse", store.ScopeRead|store.ScopeWrite)

	code, raw, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/parse",
		`{"url":"https://www.bilibili.com/video/BV1xx411c7mD"}`, secOpts{token: token})
	if code != http.StatusOK {
		t.Fatalf("Bearer 解析应返回 200，实际 %d（body: %s）", code, raw)
	}
}
