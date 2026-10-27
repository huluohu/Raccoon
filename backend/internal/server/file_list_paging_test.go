package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Test媒体库列表_分页 offset 真的生效：前端媒体库靠它翻页
// （2026-10-25 接的分页），handler 必须把 offset 透传给 store。
func Test媒体库列表_分页(t *testing.T) {
	srv, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	for i := 0; i < 3; i++ {
		name := "page" + string(rune('a'+i)) + ".mp4"
		newTestMediaFile(t, srv, name)
	}

	type listResp struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
		Total int `json:"total"`
	}

	get := func(query string) listResp {
		code, body, _ := secDoBody(t, http.MethodGet, base+"/api/v1/files"+query, "",
			secOpts{cookie: cookie, csrf: csrf})
		if code != http.StatusOK {
			t.Fatalf("媒体库列表应返回 200，实际 %d（body: %s）", code, body)
		}
		var out listResp
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("解析列表响应失败: %v（body: %s）", err, body)
		}
		return out
	}

	first := get("?limit=2&offset=0")
	if first.Total != 3 {
		t.Fatalf("total 应为 3，实际 %d", first.Total)
	}
	if len(first.Items) != 2 {
		t.Fatalf("首页应有 2 条，实际 %d", len(first.Items))
	}

	second := get("?limit=2&offset=2")
	if len(second.Items) != 1 {
		t.Fatalf("第二页应有 1 条，实际 %d", len(second.Items))
	}
	// 两页不重复
	for _, a := range first.Items {
		for _, b := range second.Items {
			if a.ID == b.ID {
				t.Fatalf("分页出现重复条目: %s", a.ID)
			}
		}
	}
}
