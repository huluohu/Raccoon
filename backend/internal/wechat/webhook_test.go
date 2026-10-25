package wechat

import (
	"crypto/sha1"
	"fmt"
	"sort"
	"strings"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	token := "raccoon_test_token"
	timestamp := "1700000000"
	nonce := "123456"

	// 正确签名
	parts := []string{token, timestamp, nonce}
	sort.Strings(parts)
	h := sha1.New()
	h.Write([]byte(strings.Join(parts, "")))
	validSig := fmt.Sprintf("%x", h.Sum(nil))

	if !VerifySignature(token, timestamp, nonce, validSig) {
		t.Fatal("正确签名校验应通过")
	}

	if VerifySignature(token, timestamp, nonce, "invalid_sig") {
		t.Fatal("错误签名校验应拒绝")
	}

	if VerifySignature("", timestamp, nonce, validSig) {
		t.Fatal("Token 为空时应拒绝")
	}
}

func TestParseMessageAndExtractURLs(t *testing.T) {
	xmlData := []byte(`<xml>
<ToUserName><![CDATA[gh_raccoon]]></ToUserName>
<FromUserName><![CDATA[user_123]]></FromUserName>
<CreateTime>1348831860</CreateTime>
<MsgType><![CDATA[text]]></MsgType>
<Content><![CDATA[快看这个视频 https://www.bilibili.com/video/BV1GJ411x7h7 超赞，还有 https://v.douyin.com/abc/ 也很好看。]]></Content>
<MsgId>1234567890123456</MsgId>
</xml>`)

	msg, err := ParseMessage(xmlData)
	if err != nil {
		t.Fatalf("ParseMessage 失败: %v", err)
	}
	if msg.FromUserName != "user_123" {
		t.Fatalf("FromUserName 应为 user_123，实际 %s", msg.FromUserName)
	}
	if msg.MsgType != "text" {
		t.Fatalf("MsgType 应为 text，实际 %s", msg.MsgType)
	}

	urls := ExtractURLs(msg.Content)
	if len(urls) != 2 {
		t.Fatalf("应提取出 2 个链接，实际提取到 %d 个: %v", len(urls), urls)
	}
	if urls[0] != "https://www.bilibili.com/video/BV1GJ411x7h7" {
		t.Fatalf("第一个链接不匹配: %s", urls[0])
	}
	if urls[1] != "https://v.douyin.com/abc/" {
		t.Fatalf("第二个链接不匹配: %s", urls[1])
	}
}

func TestFormatReplyXML(t *testing.T) {
	data, err := FormatReplyXML("user_123", "gh_raccoon", "已入队下载")
	if err != nil {
		t.Fatal(err)
	}
	xmlStr := string(data)
	if !strings.Contains(xmlStr, "user_123") || !strings.Contains(xmlStr, "已入队下载") {
		t.Fatalf("生成的回复 XML 不符合预期: %s", xmlStr)
	}
}
