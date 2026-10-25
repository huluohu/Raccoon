package server

import (
	"crypto/sha1"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/store"
)

func wechatSignature(token, timestamp, nonce string) string {
	parts := []string{token, timestamp, nonce}
	sort.Strings(parts)
	h := sha1.New()
	h.Write([]byte(strings.Join(parts, "")))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func TestWeChatWebhook_Verification(t *testing.T) {
	srv, base, _, _ := newSecEnv(t)
	_ = srv.cfg.Set("wechat.token", "my_secret_token")

	ts := "1700000000"
	nonce := "998877"
	sig := wechatSignature("my_secret_token", ts, nonce)

	// 1. 正确签名校验
	req, _ := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/api/v1/integrations/wechat/webhook?signature=%s&timestamp=%s&nonce=%s&echostr=hello_raccoon", base, sig, ts, nonce), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("正确签名应返回 200，实际 %d", resp.StatusCode)
	}

	// 2. 错误签名校验
	reqBad, _ := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/api/v1/integrations/wechat/webhook?signature=bad&timestamp=%s&nonce=%s&echostr=test", base, ts, nonce), nil)
	respBad, err := http.DefaultClient.Do(reqBad)
	if err != nil {
		t.Fatal(err)
	}
	defer respBad.Body.Close()
	if respBad.StatusCode != http.StatusForbidden {
		t.Fatalf("错误签名应返回 403，实际 %d", respBad.StatusCode)
	}
}

func TestWeChatWebhook_ReceiveMessageAndCreateTask(t *testing.T) {
	srv, base, _, _ := newSecEnv(t)
	_ = srv.cfg.Set("wechat.token", "my_secret_token")

	ts := "1700000000"
	nonce := "123456"
	sig := wechatSignature("my_secret_token", ts, nonce)

	xmlBody := `<xml>
<ToUserName><![CDATA[gh_raccoon]]></ToUserName>
<FromUserName><![CDATA[wechat_user_01]]></FromUserName>
<CreateTime>1700000000</CreateTime>
<MsgType><![CDATA[text]]></MsgType>
<Content><![CDATA[帮我下这个：https://www.bilibili.com/video/BV1GJ411x7h7]]></Content>
<MsgId>999888777</MsgId>
</xml>`

	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/api/v1/integrations/wechat/webhook?signature=%s&timestamp=%s&nonce=%s", base, sig, ts, nonce),
		strings.NewReader(xmlBody))
	req.Header.Set("Content-Type", "application/xml")

	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST 接收应返回 200，实际 %d", w.Code)
	}
	reply := w.Body.String()
	if !strings.Contains(reply, "已收到下载任务") {
		t.Fatalf("被动回复应包含「已收到下载任务」，实际: %s", reply)
	}

	// 验证任务已进入队列
	res, err := srv.store.ListTasks(store.TaskFilter{Limit: 10})
	if err != nil || len(res.Items) == 0 {
		t.Fatalf("任务应已创建入库，实际列表: %+v", res)
	}
	found := false
	for _, it := range res.Items {
		if strings.HasPrefix(it.SourceRef, "wechat:") && strings.Contains(it.URL, "BV1GJ411x7h7") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("未找到由微信创建的任务: %+v", res.Items)
	}
}
