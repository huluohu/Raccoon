package server

// qrlogin_test.go：B 站扫码登录端到端（假 passport + 真路由 + 真凭据中心）。
//
// 验收点：start 返回二维码内容；poll 各状态语义正确；确认后凭据
// 加密落库且 API 永不回 Cookie 明文。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/bilibili"
)

func qrLoginEnv(t *testing.T, pollState int) (baseURL string, cookie *http.Cookie, csrf string) {
	t.Helper()
	withFakePassport := func() {
		old := bilibili.PassportBase
		bilibili.PassportBase = fakeBiliPassport(t, pollState).URL
		t.Cleanup(func() { bilibili.PassportBase = old })
	}
	withFakePassport()
	_, baseURL, user, pass := newSecEnv(t)
	cookie, csrf = secLoginSession(t, baseURL, user, pass)
	return baseURL, cookie, csrf
}

func TestBiliQRStart(t *testing.T) {
	baseURL, cookie, csrf := qrLoginEnv(t, bilibili.CodeWaitingScan)
	code, body, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/credentials/bilibili/qrcode/start", "{}",
		secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("start 应返回 200，实际 %d（body: %s）", code, body)
	}
	var out struct {
		Data struct {
			URL       string `json:"url"`
			QrcodeKey string `json:"qrcode_key"`
			ExpiresIn int    `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("解析响应失败: %v（body: %s）", err, body)
	}
	if out.Data.URL == "" || out.Data.QrcodeKey == "" {
		t.Fatalf("二维码内容不完整: %s", body)
	}
}

func TestBiliQRPollWaitingAndScanned(t *testing.T) {
	for _, tc := range []struct {
		state int
		want  string
	}{
		{bilibili.CodeWaitingScan, "waiting_scan"},
		{bilibili.CodeScanned, "scanned"},
		{bilibili.CodeQRExpired, "expired"},
	} {
		baseURL, cookie, csrf := qrLoginEnv(t, tc.state)
		code, body, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/credentials/bilibili/qrcode/poll",
			`{"qrcode_key":"fake-key"}`, secOpts{cookie: cookie, csrf: csrf})
		if code != http.StatusOK {
			t.Fatalf("state=%d 应返回 200，实际 %d（body: %s）", tc.state, code, body)
		}
		if !strings.Contains(body, `"status":"`+tc.want+`"`) {
			t.Fatalf("state=%d 应报 %q（body: %s）", tc.state, tc.want, body)
		}
	}
}

func TestBiliQRPollSuccessStoresCredential(t *testing.T) {
	baseURL, cookie, csrf := qrLoginEnv(t, bilibili.CodeSuccess)
	code, body, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/credentials/bilibili/qrcode/poll",
		`{"qrcode_key":"fake-key"}`, secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("poll 应返回 200，实际 %d（body: %s）", code, body)
	}
	if !strings.Contains(body, `"status":"success"`) {
		t.Fatalf("应报 success（body: %s）", body)
	}
	// 明文 Cookie 绝不能出现在响应里（只回元信息 + 打码预览）。
	if strings.Contains(body, "sess-fake") || strings.Contains(body, "jct-fake") {
		t.Fatalf("响应泄漏 Cookie 明文: %s", body)
	}
	// 凭据确实入库：列表里能看到 bilibili 平台一条。
	code, listBody, _ := secDoBody(t, http.MethodGet, baseURL+"/api/v1/credentials", "",
		secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("凭据列表应返回 200，实际 %d", code)
	}
	if !strings.Contains(listBody, `"platform":"bilibili"`) {
		t.Fatalf("凭据列表应包含 bilibili 凭据: %s", listBody)
	}
}

// fakeBiliPassport 与 internal/bilibili 的包内测试同构：server 侧再造一份
// 假 passport 走真 HTTP，验证从路由到凭据落库的完整链路。
func fakeBiliPassport(t *testing.T, pollState int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/x/passport-login/web/qrcode/generate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"url":"https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=fake-key","qrcode_key":"fake-key"}}`))
	})
	mux.HandleFunc("/x/passport-login/web/qrcode/poll", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if pollState != bilibili.CodeSuccess {
			// 真实协议：状态码在内层 data.code（外层 code 恒 0）。
			_, _ = w.Write([]byte(`{"code":0,"data":{"code":` + strconv.Itoa(pollState) + `,"message":"state"}}`))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "sess-fake", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "bili_jct", Value: "jct-fake", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "DedeUserID", Value: "42", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "DedeUserID__ckMd5", Value: "md5fake", Path: "/"})
		_, _ = w.Write([]byte(`{"code":0,"data":{"code":0}}`))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}
