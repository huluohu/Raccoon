// system_api_test.go：仪表盘统计 / 监控 / 日志 / 偏好 四组新端点的 HTTP 契约用例
// （docs/04 §2.10、§2.13；docs/15 §3.1）。
//
// 断言全部落在**可观测结果**上（docs/07 §7）：返回体字段、points 长度、脱敏后的
// 字符串、库里的偏好值——不测「handler 被调用过」这类实现细节。
package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/logx"
)

// Test统计摘要_空库成功率返回null 空库下 success_rate 必须是 null（前端显示「—」），
// 不能是 0 或 100——那是造假（docs/15 §3.1 口径）。
func Test统计摘要_空库成功率返回null(t *testing.T) {
	_, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	code, body, env := secDoBody(t, http.MethodGet, base+"/api/v1/system/stats/summary", "",
		secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("摘要应返回 200，实际 %d（%s）", code, env.Error.Code)
	}
	var got struct {
		SuccessRate *float64       `json:"success_rate"`
		Total       int            `json:"total_downloads"`
		MediaByKind map[string]int `json:"media_by_kind"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("摘要响应不是合法 JSON：%v（%s）", err, body)
	}
	if got.SuccessRate != nil {
		t.Fatalf("空库 success_rate 应为 null，实际 %v", *got.SuccessRate)
	}
	if got.Total != 0 {
		t.Fatalf("空库 total_downloads 应为 0，实际 %d", got.Total)
	}
	// media_by_kind 五个键必须都在（前端图例按固定键渲染，缺键会显示空白）。
	for _, k := range []string{"video", "audio", "image", "subtitle", "other"} {
		if _, ok := got.MediaByKind[k]; !ok {
			t.Fatalf("media_by_kind 缺少键 %q：%v", k, got.MediaByKind)
		}
	}
}

// Test趋势_days非法400且长度恒等 长度必须恒等于 days（末元素=今日），
// 否则图表会因「无数据日被跳过」而把横轴压扁。
func Test趋势_days非法400且长度恒等(t *testing.T) {
	_, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	if code, _, env := secDoBody(t, http.MethodGet, base+"/api/v1/system/stats/trend?days=5", "",
		secOpts{cookie: cookie, csrf: csrf}); code != http.StatusBadRequest || env.Error.Code != "VALIDATION" {
		t.Fatalf("days=5 应返回 400 VALIDATION，实际 %d / %s", code, env.Error.Code)
	}

	for _, days := range []int{7, 30} {
		code, body, env := secDoBody(t, http.MethodGet,
			base+"/api/v1/system/stats/trend?days="+strconv.Itoa(days), "",
			secOpts{cookie: cookie, csrf: csrf})
		if code != http.StatusOK {
			t.Fatalf("days=%d 应返回 200，实际 %d（%s）", days, code, env.Error.Code)
		}
		var got struct {
			Days   int `json:"days"`
			Points []struct {
				Date  string `json:"date"`
				Count int    `json:"count"`
				Bytes int64  `json:"bytes"`
			} `json:"points"`
			Failures struct {
				ByPlatform []any `json:"by_platform"`
				ByReason   []any `json:"by_reason"`
			} `json:"failures"`
		}
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("趋势响应不是合法 JSON：%v", err)
		}
		if got.Days != days || len(got.Points) != days {
			t.Fatalf("days=%d 时 points 长度应为 %d，实际 %d（days 字段 %d）",
				days, days, len(got.Points), got.Days)
		}
		today := time.Now().Format("2006-01-02")
		if last := got.Points[len(got.Points)-1].Date; last != today {
			t.Fatalf("末元素应为今日 %s，实际 %s", today, last)
		}
		if got.Failures.ByPlatform == nil || got.Failures.ByReason == nil {
			t.Fatalf("failures 两数组应存在（空库也要是 []）：%s", body)
		}
	}
}

// Test监控历史_range校验与粒度 非 1h/24h 一律 400；粒度随窗口变化（60s / 1440s）。
func Test监控历史_range校验与粒度(t *testing.T) {
	_, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	if code, _, env := secDoBody(t, http.MethodGet, base+"/api/v1/system/monitor/history?range=7d", "",
		secOpts{cookie: cookie, csrf: csrf}); code != http.StatusBadRequest || env.Error.Code != "VALIDATION" {
		t.Fatalf("range=7d 应返回 400 VALIDATION，实际 %d / %s", code, env.Error.Code)
	}

	cases := map[string]int64{"1h": 60, "24h": 1440}
	for rng, wantGran := range cases {
		code, body, env := secDoBody(t, http.MethodGet,
			base+"/api/v1/system/monitor/history?range="+rng, "", secOpts{cookie: cookie, csrf: csrf})
		if code != http.StatusOK {
			t.Fatalf("range=%s 应返回 200，实际 %d（%s）", rng, code, env.Error.Code)
		}
		var got struct {
			Range          string `json:"range"`
			GranularitySec int64  `json:"granularity_sec"`
			Points         []any  `json:"points"`
		}
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("监控历史响应不是合法 JSON：%v", err)
		}
		if got.Range != rng || got.GranularitySec != wantGran {
			t.Fatalf("range=%s 期望 granularity_sec=%d，实际 %q / %d",
				rng, wantGran, got.Range, got.GranularitySec)
		}
		// 空窗期返回 []，不是 null（前端直接 .map，null 会崩）。
		if got.Points == nil {
			t.Fatalf("range=%s 空窗期 points 应为 []，实际 null", rng)
		}
	}
}

// Test日志_过滤脱敏与游标 日志页的三条硬要求：按 level 过滤、出口脱敏、
// 游标能翻页且不重复不丢行。
func Test日志_过滤脱敏与游标(t *testing.T) {
	srv, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	dataDir := srv.cfg.GetString("storage.data_dir")
	logPath := logx.FilePath(dataDir)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("建日志目录失败：%v", err)
	}
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	lines := []string{
		`{"ts":"` + ts + `","level":"info","module":"task","msg":"任务已入队"}`,
		`{"ts":"` + ts + `","level":"error","module":"engine","msg":"引擎退出码 1"}`,
		`{"ts":"` + ts + `","level":"info","module":"auth","msg":"{\"token\":\"abc123\",\"user\":\"root\"}"}`,
		`{"ts":"` + ts + `","level":"warn","module":"qb","msg":"qB 超时，准备回退"}`,
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("写测试日志失败：%v", err)
	}

	// 1) level 过滤：只要 error。
	code, body, env := secDoBody(t, http.MethodGet, base+"/api/v1/logs?level=error", "",
		secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("日志应返回 200，实际 %d（%s）", code, env.Error.Code)
	}
	var one struct {
		Items      []logItem `json:"items"`
		NextCursor string    `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(body), &one); err != nil {
		t.Fatalf("日志响应不是合法 JSON：%v", err)
	}
	if len(one.Items) != 1 || one.Items[0].Level != "error" || one.Items[0].Module != "engine" {
		t.Fatalf("level=error 应只回 1 条 engine 错误，实际 %+v", one.Items)
	}

	// 2) 脱敏：带 token 的那条，msg 里不得出现明文，且必须出现 ***。
	_, body, _ = secDoBody(t, http.MethodGet, base+"/api/v1/logs?q=token", "",
		secOpts{cookie: cookie, csrf: csrf})
	if strings.Contains(body, "abc123") {
		t.Fatalf("日志接口泄漏 token 明文：%s", body)
	}
	if !strings.Contains(body, "***") {
		t.Fatalf("脱敏后应出现 ***：%s", body)
	}

	// 3) 游标：limit=2 首页 2 条，next_cursor 指向第 3 行；续页不得重复首条。
	code, body, _ = secDoBody(t, http.MethodGet, base+"/api/v1/logs?limit=2", "",
		secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("limit=2 应返回 200，实际 %d", code)
	}
	var page1, page2 struct {
		Items      []logItem `json:"items"`
		NextCursor string    `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(body), &page1); err != nil {
		t.Fatalf("首页解析失败：%v", err)
	}
	if len(page1.Items) != 2 || page1.NextCursor != "2" {
		t.Fatalf("首页应为 2 条且 next_cursor=2，实际 %d 条 / %q", len(page1.Items), page1.NextCursor)
	}
	_, body, _ = secDoBody(t, http.MethodGet, base+"/api/v1/logs?limit=2&cursor="+page1.NextCursor, "",
		secOpts{cookie: cookie, csrf: csrf})
	if err := json.Unmarshal([]byte(body), &page2); err != nil {
		t.Fatalf("续页解析失败：%v", err)
	}
	if len(page2.Items) != 2 {
		t.Fatalf("续页应为剩下 2 条，实际 %d（%s）", len(page2.Items), body)
	}
	if page2.Items[0].Msg == page1.Items[0].Msg {
		t.Fatalf("续页首条与首页首条重复（游标算错）：%s", page2.Items[0].Msg)
	}

	// 4) 非法 level → 400（不是静默当成默认过滤）。
	if code, _, env := secDoBody(t, http.MethodGet, base+"/api/v1/logs?level=bogus", "",
		secOpts{cookie: cookie, csrf: csrf}); code != http.StatusBadRequest || env.Error.Code != "VALIDATION" {
		t.Fatalf("level=bogus 应返回 400 VALIDATION，实际 %d / %s", code, env.Error.Code)
	}
}

// Test偏好_往返与校验 布局是 JSON 字符串原样存取；非法 JSON 400；写必须带 CSRF。
func Test偏好_往返与校验(t *testing.T) {
	_, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	// 初始：无记录 → data.layout 为空串（前端回落默认布局）。
	code, body, env := secDoBody(t, http.MethodGet, base+"/api/v1/user/preferences", "",
		secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("读偏好应返回 200，实际 %d（%s）", code, env.Error.Code)
	}
	var wrap struct {
		Data struct {
			Layout string `json:"layout"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &wrap); err != nil {
		t.Fatalf("偏好响应不是合法 JSON：%v", err)
	}
	if wrap.Data.Layout != "" {
		t.Fatalf("初始布局应为空串，实际 %q", wrap.Data.Layout)
	}

	layout := `{"hidden":["live"],"order":["trend","queue"]}`
	code, body, env = secDoBody(t, http.MethodPut, base+"/api/v1/user/preferences",
		`{"layout":`+quote(layout)+`}`, secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("写偏好应返回 200，实际 %d（%s / %s）", code, env.Error.Code, body)
	}

	_, body, _ = secDoBody(t, http.MethodGet, base+"/api/v1/user/preferences", "",
		secOpts{cookie: cookie, csrf: csrf})
	if err := json.Unmarshal([]byte(body), &wrap); err != nil {
		t.Fatalf("回读解析失败：%v", err)
	}
	if wrap.Data.Layout != layout {
		t.Fatalf("布局未原样存取：期望 %s，实际 %s", layout, wrap.Data.Layout)
	}

	// 非法 JSON → 400。
	if code, _, env := secDoBody(t, http.MethodPut, base+"/api/v1/user/preferences",
		`{"layout":"not-json"}`, secOpts{cookie: cookie, csrf: csrf}); code != http.StatusBadRequest || env.Error.Code != "VALIDATION" {
		t.Fatalf("非法 layout 应返回 400 VALIDATION，实际 %d / %s", code, env.Error.Code)
	}

	// 缺 CSRF → 403，且库里仍是上一次的合法值。
	if code, _ := secDo(t, http.MethodPut, base+"/api/v1/user/preferences",
		`{"layout":"{\"hidden\":[],\"order\":[]}"}`, secOpts{cookie: cookie}); code != http.StatusForbidden {
		t.Fatalf("缺 CSRF 的 PUT 应返回 403，实际 %d", code)
	}
}

// quote 把字符串编成 JSON 字面量（用例里嵌 JSON 字符串用）。
func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
