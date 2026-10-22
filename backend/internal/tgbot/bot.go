package tgbot

// bot.go：命令分发器（协议层与业务层的接缝）。
//
// 职责：轮询拉取 update → 去重 → 鉴权 → 解析命令 → 调 Handler → 回话。
// 除此之外什么都不做：不下载、不建任务、不查媒体库——那些都在上层。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/huluohu/raccoon/internal/logx"
)

// Bot 命令分发器。
type Bot struct {
	client  *Client
	handler Handler
	log     *logx.Logger

	// mu 保护 offset 与 seen（offset 由轮询循环写、可能被外部查询）。
	mu     sync.Mutex
	offset int64
	seen   *seenSet

	// pollTimeout 长轮询等待时长。
	pollTimeout time.Duration
	// errBackoff 轮询出错后的等待时长（避免错误时把 Telegram 打挂）。
	errBackoff time.Duration
	// errorBackoff 出错等待的基数，指数退避。
	errorBackoff time.Duration
}

// BotOption 可选参数。
type BotOption func(*Bot)

// WithPollTimeout 设置长轮询等待时长。
func WithPollTimeout(d time.Duration) BotOption {
	return func(b *Bot) { b.pollTimeout = d }
}

// WithErrorBackoff 设置轮询出错后的退避基数。
func WithErrorBackoff(d time.Duration) BotOption {
	return func(b *Bot) { b.errorBackoff = d }
}

// WithDedupCapacity 设置去重窗口容量。
func WithDedupCapacity(n int) BotOption {
	return func(b *Bot) { b.seen = newSeenSet(n) }
}

// NewBot 构造分发器。
//
// handler 为 nil 时仍可构造（只做鉴权与回话），但收到命令会回「未配置」。
func NewBot(client *Client, handler Handler, opts ...BotOption) *Bot {
	b := &Bot{
		client:       client,
		handler:      handler,
		log:          client.log,
		seen:         newSeenSet(0),
		pollTimeout:  client.cfg.PollTimeout,
		errorBackoff: client.cfg.RetryBackoff,
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.errorBackoff <= 0 {
		b.errorBackoff = time.Second
	}
	return b
}

// Offset 返回当前 offset（已确认处理到的 update_id + 1）。
func (b *Bot) Offset() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.offset
}

// advanceOffset 把 offset 推进到 update_id+1（只增不减）。
//
// 必须推进到「最大 update_id + 1」而不是当前这条 +1：Telegram 按 offset
// 确认消费，offset 停在一半会让已处理的 update 反复下发。
func (b *Bot) advanceOffset(updateID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if updateID+1 > b.offset {
		b.offset = updateID + 1
	}
}

// Run 轮询循环，直到 ctx 取消。
//
// 出错不退出：网络抖动、限流都是常态，退避后继续。只有 ctx 取消才返回。
func (b *Bot) Run(ctx context.Context) error {
	backoff := b.errorBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		updates, err := b.client.GetUpdates(ctx, b.Offset(), b.pollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.log.Error("拉取 Telegram 更新失败", map[string]any{
				"err":        err.Error(),
				"backoff_ms": backoff.Milliseconds(),
			})
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			continue
		}
		backoff = b.errorBackoff
		for i := range updates {
			// 先推进 offset 再处理：Telegram 按 offset 确认消费，
			// 若处理失败却不推进，同一条 update 会被无限重发，把队列堵死。
			// 失败的 update 由日志记录，不重试。
			b.advanceOffset(updates[i].UpdateID)
			if err := b.HandleUpdate(ctx, updates[i]); err != nil {
				// 单条 update 处理失败不应该中断轮询：记录后继续。
				// 重复 update 是正常现象（Telegram 会重投），不记警告避免刷屏。
				if !errors.Is(err, ErrDuplicateUpdate) {
					b.log.Warn("处理 Telegram 更新出错", map[string]any{
						"update_id": updates[i].UpdateID,
						"err":       err.Error(),
					})
				}
			}
		}
	}
}

// HandleUpdate 处理一条入站更新（对外暴露，便于测试与上层手动注入）。
//
// 顺序：去重 → 取消息 → 鉴权 → 解析命令 → 分发 → 回话。
// 鉴权放在解析之前：未授权用户不该看到命令帮助（那是信息泄露）。
func (b *Bot) HandleUpdate(ctx context.Context, u Update) error {
	// 去重：同一 update_id 只处理一次。
	b.mu.Lock()
	dup := b.seen.Seen(u.UpdateID)
	b.mu.Unlock()
	if dup {
		return ErrDuplicateUpdate
	}

	// 回调查询（内联按钮）优先：它没有文本，走独立的应答路径。
	if u.CallbackQuery != nil {
		return b.handleCallbackQuery(ctx, u.CallbackQuery)
	}

	msg := u.EffectiveMessage()
	if msg == nil {
		return nil
	}
	user := msg.From
	chatID := msg.Chat.ID
	if user == nil {
		// 没有发送者的消息（频道推送等）无法鉴权，直接忽略。
		return nil
	}
	// 鉴权：用户白名单 + 会话白名单。
	if !b.client.IsUserAllowed(user.ID) || !b.client.IsChatAllowed(chatID) {
		b.log.Warn("拒绝未授权的 Telegram 请求", map[string]any{
			"user_id": user.ID,
			"chat_id": chatID,
		})
		// 回一句拒绝：让对方知道「机器人收到了但不放行」，
		// 而不是让对方反复重试。
		_ = b.reply(ctx, chatID, "未授权：该账号不在白名单内，无法使用此机器人。")
		return ErrUnauthorized
	}

	req, err := ParseCommand(msg.Text)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCommand):
			_ = b.reply(ctx, chatID, "未知命令。\n\n"+HelpText())
			return err
		case errors.Is(err, ErrMissingArgument):
			_ = b.reply(ctx, chatID, "参数缺失。\n\n"+HelpText())
			return err
		default:
			return err
		}
	}
	if req == nil {
		// 不是命令：普通聊天内容，忽略（Bot 不做闲聊）。
		return nil
	}
	return b.dispatch(ctx, chatID, user, req)
}

// dispatch 按命令分发给 Handler。
func (b *Bot) dispatch(ctx context.Context, chatID int64, user *User, req *CommandRequest) error {
	if b.handler == nil {
		_ = b.reply(ctx, chatID, "机器人尚未配置业务处理器，暂时无法处理该命令。")
		return ErrNoHandler
	}
	var (
		text string
		err  error
	)
	switch req.Command {
	case CmdStart:
		err = b.handler.OnStart(ctx, chatID, user)
		if err == nil {
			text = HelpText()
		}
	case CmdHelp:
		text = HelpText()
	case CmdAdd:
		text, err = b.handler.OnAdd(ctx, chatID, user, req.Arg)
	case CmdStatus:
		text, err = b.handler.OnStatus(ctx, chatID, user, req.Arg)
	case CmdList:
		text, err = b.handler.OnList(ctx, chatID, user)
	case CmdCancel:
		text, err = b.handler.OnCancel(ctx, chatID, user, req.Arg)
	case CmdMedia:
		text, err = b.handler.OnMedia(ctx, chatID, user, req.Arg)
	default:
		return ErrInvalidCommand
	}
	if err != nil {
		b.log.Error("命令处理失败", map[string]any{
			"command": string(req.Command),
			"user_id": user.ID,
			"err":     err.Error(),
		})
		// 回错误摘要：用户在聊天窗口里等回复，没回声会以为机器人挂了。
		_ = b.reply(ctx, chatID, "命令执行失败："+truncate(err.Error(), 200))
		return err
	}
	if text != "" {
		_ = b.reply(ctx, chatID, text)
	}
	return nil
}

// handleCallbackQuery 处理内联按钮回调。
//
// 先应答（Telegram 客户端会一直转圈直到收到应答），再把按钮携带的
// data 当作命令文本分发——这样按钮和文本命令共用同一套语义。
func (b *Bot) handleCallbackQuery(ctx context.Context, cq *CallbackQuery) error {
	user := cq.From
	chatID := int64(0)
	if cq.Message != nil {
		chatID = cq.Message.Chat.ID
	}
	if user != nil && chatID != 0 {
		if !b.client.IsUserAllowed(user.ID) || !b.client.IsChatAllowed(chatID) {
			_ = b.client.AnswerCallbackQuery(ctx, cq.ID, "未授权")
			return ErrUnauthorized
		}
	}
	if err := b.client.AnswerCallbackQuery(ctx, cq.ID, ""); err != nil {
		// 应答失败只记日志：数据可能已经处理过，不该因此重复执行。
		b.log.Warn("应答 Telegram 回调失败", map[string]any{"err": err.Error()})
	}
	// 缺少发送者或会话时只应答不分发：Handler 契约要求两者都在。
	if user == nil || chatID == 0 {
		return fmt.Errorf("tgbot: 回调 %s 缺少发送者或 chat_id", cq.ID)
	}
	req, err := ParseCommand(cq.Data)
	if err != nil || req == nil {
		return err
	}
	return b.dispatch(ctx, chatID, user, req)
}

// reply 回一条纯文本消息。
//
// 固定用纯文本（不带 parse_mode）：Bot 内部生成的内容里含用户数据
// （任务名、文件名、检索词），一旦命中 Markdown 语法就会被 Telegram 拒收。
func (b *Bot) reply(ctx context.Context, chatID int64, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	_, err := b.client.SendMessage(ctx, chatID, text, ParseModeNone)
	return err
}
