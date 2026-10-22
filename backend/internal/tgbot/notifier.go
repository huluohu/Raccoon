package tgbot

// notifier.go：内置的通知出口实现。
//
// 上层也可以自己实现 Notifier 接口（比如同时写通知中心 + 推 Telegram），
// 这里给一份「直接把结果推回聊天窗口」的默认实现，方便上层零成本接上。

import (
	"context"
	"fmt"
	"strings"
)

// MessageNotifier 默认通知出口：把任务结果推回发起命令的聊天窗口。
type MessageNotifier struct {
	client *Client
}

// NewMessageNotifier 构造默认通知出口。
func NewMessageNotifier(client *Client) *MessageNotifier {
	return &MessageNotifier{client: client}
}

// NotifyTaskDone 推送任务完成。
//
// 文案里带上任务 ID：用户要拿 ID 去查详情或取消，缺了就没法自助。
func (n *MessageNotifier) NotifyTaskDone(ctx context.Context, chatID int64, taskID, title, body string) error {
	msg := formatNotify("✅ 任务完成", taskID, title, body)
	_, err := n.client.SendMessage(ctx, chatID, msg, ParseModeNone)
	return err
}

// NotifyTaskFailed 推送任务失败。
func (n *MessageNotifier) NotifyTaskFailed(ctx context.Context, chatID int64, taskID, title, body string) error {
	msg := formatNotify("❌ 任务失败", taskID, title, body)
	_, err := n.client.SendMessage(ctx, chatID, msg, ParseModeNone)
	return err
}

// formatNotify 拼装通知文案。
func formatNotify(headline, taskID, title, body string) string {
	var sb strings.Builder
	sb.WriteString(headline)
	if title != "" {
		sb.WriteString("\n")
		sb.WriteString(title)
	}
	if body != "" {
		sb.WriteString("\n")
		sb.WriteString(body)
	}
	if taskID != "" {
		sb.WriteString(fmt.Sprintf("\n任务 ID: %s", taskID))
	}
	return sb.String()
}
