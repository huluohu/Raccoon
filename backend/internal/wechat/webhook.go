package wechat

// webhook.go：微信公众平台 / 订阅号 / 服务号 / 企业微信 Webhook 协议处理。
//
// 机制说明：
//   1. 接入验证（GET）：微信服务器通过 signature / timestamp / nonce 校验 token，校验通过返回 echostr；
//   2. 接收消息（POST）：微信推送 XML 格式文本消息，提取其中的 HTTP/HTTPS 视频链接并自动投递任务队列；
//   3. 被动响应：返回 XML 格式文本消息，即时告知用户任务已受理入队，无需任何复杂外部中间件。

import (
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// InMessage 微信推送消息的 XML 结构。
type InMessage struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
	MsgID        string   `xml:"MsgId"`
}

// OutTextMessage 回复用户的 XML 文本消息。
type OutTextMessage struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
}

// VerifySignature 校验微信服务器签名的有效性。
// 算法：将 token、timestamp、nonce 三个参数字典序排序后 SHA1 加密，与 signature 比对。
func VerifySignature(token, timestamp, nonce, signature string) bool {
	if token == "" || signature == "" {
		return false
	}
	strs := []string{token, timestamp, nonce}
	sort.Strings(strs)
	h := sha1.New()
	h.Write([]byte(strings.Join(strs, "")))
	return fmt.Sprintf("%x", h.Sum(nil)) == signature
}

// ParseMessage 解析微信推送的 XML 消息包。
func ParseMessage(body []byte) (*InMessage, error) {
	var msg InMessage
	if err := xml.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("解析微信 XML 消息失败: %w", err)
	}
	return &msg, nil
}

var urlPattern = regexp.MustCompile(`https?://[^\s\x{4e00}-\x{9fa5}]+`)

// ExtractURLs 从文本中提取所有合法的 HTTP/HTTPS 链接（自动过滤中文字符与首尾标点）。
func ExtractURLs(text string) []string {
	matches := urlPattern.FindAllString(text, -1)
	var out []string
	seen := make(map[string]bool)
	for _, m := range matches {
		m = strings.TrimRight(m, "。，、；！？）)》\"'")
		if m != "" && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// FormatReplyXML 格式化生成被动回复的 XML 消息。
func FormatReplyXML(toUser, fromUser, content string) ([]byte, error) {
	msg := OutTextMessage{
		ToUserName:   toUser,
		FromUserName: fromUser,
		CreateTime:   time.Now().Unix(),
		MsgType:      "text",
		Content:      content,
	}
	return xml.Marshal(msg)
}
