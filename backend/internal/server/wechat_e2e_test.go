package server

// wechat_e2e_test.go：微信远程下载的服务端端到端验证。
//
// 覆盖微信公众平台的完整协议面：
//   - 接入验证（GET）：签名校验通过回 echostr，签名错回 403；
//   - 消息接收（POST）：文本消息里的链接应真的建成任务；
//   - 被动回复：必须回 XML（含任务条数），否则微信会重投轰炸；
//   - 非文本消息 / 无链接：回引导语而不是报错；
//   - 解析失败：按微信要求回 "success"，不制造重试风暴。

import (
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/store"
)

// wechatSign 按微信算法算签名（token+timestamp+nonce 字典序拼接后 SHA1）。
func wechatSign(token, timestamp, nonce string) string {
	strs := []string{token, timestamp, nonce}
	sort.Strings(strs)
	h := sha1.New()
	h.Write([]byte(strings.Join(strs, "")))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// wechatTextMsg 造一条微信文本消息 XML。
func wechatTextMsg(from, to, content string) string {
	return fmt.Sprintf(`<xml>
<ToUserName><![CDATA[%s]]></ToUserName>
<FromUserName><![CDATA[%s]]></FromUserName>
<CreateTime>%d</CreateTime>
<MsgType><![CDATA[text]]></MsgType>
<Content><![CDATA[%s]]></Content>
<MsgId>1234567890123456</MsgId>
</xml>`, to, from, 1700000000, content)
}

// wechatReply 解析被动回复 XML。
type wechatReply struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
}

// TestWeChat_签名错被拒 接入验证与消息接收都要校验签名。
func TestWeChat_签名错被拒(t *testing.T) {
	srv, baseURL, _, _ := newSecEnv(t)
	if err := srv.cfg.Set("wechat.token", "my-token"); err != nil {
		t.Fatalf("设置 token 失败: %v", err)
	}

	// 签名错误
	code, _, _ := secDoBody(t, http.MethodGet, baseURL+"/api/v1/integrations/wechat/webhook?signature=deadbeef&timestamp=1&nonce=2", "", secOpts{})
	if code != http.StatusForbidden {
		t.Fatalf("签名错误应返回 403，实际 %d", code)
	}
	// 消息接口同样要验签
	body := wechatTextMsg("user1", "gh_xxx", "https://www.bilibili.com/video/BV1xx411c7mD")
	code, _, _ = secDoBody(t, http.MethodPost,
		baseURL+"/api/v1/integrations/wechat/webhook?signature=deadbeef&timestamp=1&nonce=2",
		body, secOpts{})
	if code != http.StatusForbidden {
		t.Fatalf("签名错误的消息应返回 403，实际 %d", code)
	}
}

// TestWeChat_接入验证返回echostr 签名正确时 GET 应回 echostr。
func TestWeChat_接入验证返回echostr(t *testing.T) {
	srv, baseURL, _, _ := newSecEnv(t)
	_ = srv.cfg.Set("wechat.token", "my-token")
	sig := wechatSign("my-token", "1700000000", "abc")
	code, raw, _ := secDoBody(t, http.MethodGet,
		baseURL+"/api/v1/integrations/wechat/webhook?signature="+sig+"&timestamp=1700000000&nonce=abc&echostr=hello123",
		"", secOpts{})
	if code != http.StatusOK {
		t.Fatalf("接入验证应 200，实际 %d（%s）", code, raw)
	}
	// 微信的接入验证要求原样回显 echostr——回错/回空，平台侧就配不上。
	if raw != "hello123" {
		t.Errorf("接入验证应原样回显 echostr（hello123），实际 %q", raw)
	}
}

// TestWeChat_文本链接真的建了任务 微信发来链接 → 库里多一条任务。
func TestWeChat_文本链接真的建了任务(t *testing.T) {
	srv, baseURL, _, _ := newSecEnv(t)
	_ = srv.cfg.Set("wechat.token", "my-token")
	sig := wechatSign("my-token", "1700000000", "abc")
	url := baseURL + "/api/v1/integrations/wechat/webhook?signature=" + sig + "&timestamp=1700000000&nonce=abc"

	before, err := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	if err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}

	body := wechatTextMsg("user1", "gh_xxx", "帮我下 https://www.bilibili.com/video/BV1xx411c7mD 谢谢")
	code, raw, _ := secDoBody(t, http.MethodPost, url, body, secOpts{})
	if code != http.StatusOK {
		t.Fatalf("微信消息应 200，实际 %d（%s）", code, raw)
	}

	after, err := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	if err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if len(after.Items) != len(before.Items)+1 {
		t.Fatalf("应建 1 条任务，之前 %d 现在 %d", len(before.Items), len(after.Items))
	}

	// 被动回复：必须是 XML，且说明已收到。
	var reply wechatReply
	if err := xml.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatalf("回复应是合法 XML: %v（%s）", err, raw)
	}
	if reply.ToUserName != "user1" || reply.FromUserName != "gh_xxx" {
		t.Errorf("回复的收发方应互换，实际 to=%q from=%q", reply.ToUserName, reply.FromUserName)
	}
	if !strings.Contains(reply.Content, "已收到下载任务") {
		t.Errorf("回复应说明任务已收到，实际 %q", reply.Content)
	}
	if !strings.Contains(reply.Content, "1") {
		t.Errorf("回复应带任务条数，实际 %q", reply.Content)
	}
	t.Logf("微信回复: %q", reply.Content)
}

// TestWeChat_多个链接建多个任务 一条消息里多个链接应全部入队。
func TestWeChat_多个链接建多个任务(t *testing.T) {
	srv, baseURL, _, _ := newSecEnv(t)
	_ = srv.cfg.Set("wechat.token", "my-token")
	sig := wechatSign("my-token", "1700000000", "abc")
	url := baseURL + "/api/v1/integrations/wechat/webhook?signature=" + sig + "&timestamp=1700000000&nonce=abc"

	before, _ := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	body := wechatTextMsg("user1", "gh_xxx",
		"这两个都要 https://www.bilibili.com/video/BV1xx411c7mD 和 https://www.bilibili.com/video/BV1yy411c7mD")
	code, raw, _ := secDoBody(t, http.MethodPost, url, body, secOpts{})
	if code != http.StatusOK {
		t.Fatalf("应 200，实际 %d", code)
	}
	after, _ := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	if len(after.Items) != len(before.Items)+2 {
		t.Fatalf("应建 2 条任务，之前 %d 现在 %d", len(before.Items), len(after.Items))
	}
	if !strings.Contains(raw, "共 2 个") {
		t.Errorf("回复应说明共 2 个，实际 %q", raw)
	}
}

// TestWeChat_无链接回引导语 消息里没有链接时不该报错，也不该建任务。
func TestWeChat_无链接回引导语(t *testing.T) {
	srv, baseURL, _, _ := newSecEnv(t)
	_ = srv.cfg.Set("wechat.token", "my-token")
	sig := wechatSign("my-token", "1700000000", "abc")
	url := baseURL + "/api/v1/integrations/wechat/webhook?signature=" + sig + "&timestamp=1700000000&nonce=abc"

	before, _ := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	body := wechatTextMsg("user1", "gh_xxx", "你好")
	code, raw, _ := secDoBody(t, http.MethodPost, url, body, secOpts{})
	if code != http.StatusOK {
		t.Fatalf("应 200，实际 %d", code)
	}
	if !strings.Contains(raw, "未检测到有效的视频链接") {
		t.Errorf("应回引导语，实际 %q", raw)
	}
	after, _ := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	if len(after.Items) != len(before.Items) {
		t.Fatalf("无链接不该建任务（%d → %d）", len(before.Items), len(after.Items))
	}
}

// TestWeChat_非文本消息回引导语 图片/语音等消息类型不该被当成下载请求。
func TestWeChat_非文本消息回引导语(t *testing.T) {
	srv, baseURL, _, _ := newSecEnv(t)
	_ = srv.cfg.Set("wechat.token", "my-token")
	sig := wechatSign("my-token", "1700000000", "abc")
	url := baseURL + "/api/v1/integrations/wechat/webhook?signature=" + sig + "&timestamp=1700000000&nonce=abc"

	body := `<xml><ToUserName><![CDATA[gh_xxx]]></ToUserName>
<FromUserName><![CDATA[user1]]></FromUserName>
<CreateTime>1700000000</CreateTime>
<MsgType><![CDATA[image]]></MsgType>
<MsgId>1</MsgId></xml>`
	code, raw, _ := secDoBody(t, http.MethodPost, url, body, secOpts{})
	if code != http.StatusOK {
		t.Fatalf("应 200，实际 %d", code)
	}
	if !strings.Contains(raw, "请向我直接发送视频分享链接") {
		t.Errorf("非文本消息应回引导语，实际 %q", raw)
	}
}

// TestWeChat_解析失败回success 微信要求解析失败也回 success，否则会重投轰炸。
func TestWeChat_解析失败回success(t *testing.T) {
	srv, baseURL, _, _ := newSecEnv(t)
	_ = srv.cfg.Set("wechat.token", "my-token")
	sig := wechatSign("my-token", "1700000000", "abc")
	url := baseURL + "/api/v1/integrations/wechat/webhook?signature=" + sig + "&timestamp=1700000000&nonce=abc"

	code, raw, _ := secDoBody(t, http.MethodPost, url, "not-xml-at-all", secOpts{})
	if code != http.StatusOK {
		t.Fatalf("解析失败应回 200，实际 %d", code)
	}
	if !strings.Contains(raw, "success") {
		t.Errorf("解析失败应回 success，实际 %q", raw)
	}
}

// TestWeChat_内网链接不建任务 微信发来的链接同样要过 SSRF 校验。
func TestWeChat_内网链接不建任务(t *testing.T) {
	srv, baseURL, _, _ := newSecEnv(t)
	_ = srv.cfg.Set("wechat.token", "my-token")
	sig := wechatSign("my-token", "1700000000", "abc")
	url := baseURL + "/api/v1/integrations/wechat/webhook?signature=" + sig + "&timestamp=1700000000&nonce=abc"

	before, _ := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	body := wechatTextMsg("user1", "gh_xxx", "http://169.254.169.254/latest/meta-data")
	code, raw, _ := secDoBody(t, http.MethodPost, url, body, secOpts{})
	if code != http.StatusOK {
		t.Fatalf("应 200（微信要拿到回执），实际 %d", code)
	}
	after, _ := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	if len(after.Items) != len(before.Items) {
		t.Fatalf("不该为内网地址建任务（%d → %d）", len(before.Items), len(after.Items))
	}
	if !strings.Contains(raw, "不被允许") {
		t.Errorf("回复应说明链接不被允许（防 SSRF），实际 %q", raw)
	}
}
