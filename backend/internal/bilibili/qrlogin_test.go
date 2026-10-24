package bilibili

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakePassport 起一个假 B 站 passport 服务（generate 固定二维码、poll 按 state 回状态）。
func fakePassport(t *testing.T, pollState int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/x/passport-login/web/qrcode/generate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"url":"https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=fake-key","qrcode_key":"fake-key"}}`))
	})
	mux.HandleFunc("/x/passport-login/web/qrcode/poll", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("qrcode_key") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if pollState != CodeSuccess {
			// 真实协议：状态码在内层 data.code（外层 code 恒 0）。
			_, _ = w.Write([]byte(`{"code":0,"data":{"code":` + itoa(pollState) + `,"message":"state"}}`))
			return
		}
		// 成功：登录 Cookie 走 Set-Cookie（与真实协议一致）。
		http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "sess-fake", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "bili_jct", Value: "jct-fake", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "DedeUserID", Value: "42", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "DedeUserID__ckMd5", Value: "md5fake", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "buvid3", Value: "noise-should-drop", Path: "/"})
		_, _ = w.Write([]byte(`{"code":0,"data":{"code":0,"refresh_token":"rt"}}`))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func withFakePassport(t *testing.T, pollState int) {
	t.Helper()
	old := PassportBase
	PassportBase = fakePassport(t, pollState).URL
	t.Cleanup(func() { PassportBase = old })
}

func TestGenerate(t *testing.T) {
	withFakePassport(t, CodeWaitingScan)
	c := NewClient(nil)
	start, err := c.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	if start.QrcodeKey == "" || !strings.Contains(start.URL, "qrcode_key") {
		t.Fatalf("二维码内容不完整: %+v", start)
	}
}

func TestPollStates(t *testing.T) {
	cases := []struct {
		state  int
		status string
	}{
		{CodeWaitingScan, "未扫码"},
		{CodeScanned, "已扫"},
		{CodeQRExpired, "过期"},
	}
	for _, tc := range cases {
		withFakePassport(t, tc.state)
		res, err := NewClient(nil).Poll(context.Background(), "fake-key")
		if err != nil {
			t.Fatalf("state=%d Poll 失败: %v", tc.state, err)
		}
		if res.Code != tc.state {
			t.Fatalf("state=%d 实际 code=%d", tc.state, res.Code)
		}
		if res.Cookie != "" {
			t.Fatalf("state=%d 不该有 Cookie", tc.state)
		}
	}
}

func TestPollSuccessPicksLoginCookies(t *testing.T) {
	withFakePassport(t, CodeSuccess)
	res, err := NewClient(nil).Poll(context.Background(), "fake-key")
	if err != nil {
		t.Fatalf("Poll 失败: %v", err)
	}
	for _, want := range []string{"SESSDATA=sess-fake", "bili_jct=jct-fake", "DedeUserID=42"} {
		if !strings.Contains(res.Cookie, want) {
			t.Fatalf("Cookie 缺 %q，实际 %q", want, res.Cookie)
		}
	}
	if strings.Contains(res.Cookie, "buvid3") {
		t.Fatalf("设备指纹 Cookie 不该入库: %q", res.Cookie)
	}
}

func TestPollEmptyKey(t *testing.T) {
	if _, err := NewClient(nil).Poll(context.Background(), "  "); err == nil {
		t.Fatal("空 qrcode_key 应报错")
	}
}
