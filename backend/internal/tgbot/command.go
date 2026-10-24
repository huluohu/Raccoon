package tgbot

// command.go：命令解析。
//
// 解析只做「文本 → 结构化意图」，不碰业务、不发起任何 IO：
// 上层拿到 Command 之后决定要不要提交下载任务。这样命令解析可以纯函数测试。

import (
	"regexp"
	"strings"
)

// urlPattern 提取消息里的可下载链接（http/https 与磁力）。
// 「直接把链接发到聊天里就能下载」是 Bot 的核心体验——比 /add 更顺手；
// 一条消息里贴多个链接 = 批量提交（SaveXTube 功能墙的对齐项）。
var urlPattern = regexp.MustCompile(`https?://[^\s<>"']+|magnet:\?xt=urn:btih:[A-Za-z0-9]+`)

// ExtractURLs 从自由文本中提取全部可下载链接（去重，保序）。
func ExtractURLs(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	hits := urlPattern.FindAllString(text, -1)
	out := make([]string, 0, len(hits))
	seen := map[string]bool{}
	trailing := func(r rune) bool {
		return strings.ContainsRune(".,;:!?)、。）），", r)
	}
	for _, h := range hits {
		h = strings.TrimRightFunc(h, trailing)
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// Command 命令名。
type Command string

const (
	// CmdStart /start 打招呼。
	CmdStart Command = "start"
	// CmdHelp /help 帮助。
	CmdHelp Command = "help"
	// CmdStatus /status [taskID] 查询任务状态。
	CmdStatus Command = "status"
	// CmdList /list 列出任务。
	CmdList Command = "list"
	// CmdAdd /add <url> 提交下载任务。
	CmdAdd Command = "add"
	// CmdCancel /cancel <id> 取消任务。
	CmdCancel Command = "cancel"
	// CmdMedia /media <query> 媒体库检索。
	CmdMedia Command = "media"
)

// CommandRequest 一条解析后的命令请求。
type CommandRequest struct {
	// Command 命令类型。
	Command Command
	// Arg 位置参数，按命令语义取值：
	//   - /add    → 下载链接（取首个词，链接不含空格）
	//   - /cancel → 任务 ID
	//   - /status → 任务 ID（可空，表示「概览」）
	//   - /media  → 检索词（取整段，检索词天然可能是多词）
	Arg string
	// Raw 参数区原文（去掉命令名与首尾空白），多词时的完整内容。
	Raw string
}

// commandAliases 命令别名 → 标准命令名。
//
// Telegram 允许 `/add@MyBot` 这种带 bot 后缀的写法（群里别人 @ 了本 bot），
// 解析时统一剥掉后缀。
var commandAliases = map[string]Command{
	"start":  CmdStart,
	"help":   CmdHelp,
	"status": CmdStatus,
	"list":   CmdList,
	"add":    CmdAdd,
	"cancel": CmdCancel,
	"media":  CmdMedia,
}

// requiresArg 哪些命令必须带参数。
var requiresArg = map[Command]bool{
	CmdAdd:    true,
	CmdCancel: true,
	CmdMedia:  true,
}

// ParseCommand 解析消息文本成命令请求。
//
// 识别规则：
//   - 必须以 "/" 开头，否则返回 nil（普通聊天内容，不是命令）；
//   - 命令名大小写不敏感，支持 "/add@botname" 的带后缀写法；
//   - 参数按空白切分，第一个词作为 Arg，其余拼进 Raw（/media 的检索词
//     可能是多词，整段保留才有意义）。
//
// 返回 ErrInvalidCommand 表示「看起来像命令但不认识」，
// 返回 ErrMissingArgument 表示「命令认识但缺参数」——两者要区分开：
// 前者应回「未知命令」，后者应回「用法提示」。
func ParseCommand(text string) (*CommandRequest, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") {
		return nil, nil
	}
	// 去掉开头的 "/"，再按空白切成「命令名 + 参数区」。
	body := trimmed[1:]
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return nil, nil
	}
	name := fields[0]
	// 剥掉 @botname 后缀。
	if i := strings.IndexByte(name, '@'); i >= 0 {
		name = name[:i]
	}
	name = strings.ToLower(name)
	cmd, ok := commandAliases[name]
	if !ok {
		return nil, ErrInvalidCommand
	}
	// 参数区 = 第一个词（命令名，可能带 @botname 后缀）之后的全部内容。
	//
	// 这里按「第一个空白」切，而不是按 name 的长度切：/add@BotName 里 name 是
	// "add"，但参数起点在 "@BotName" 之后，按长度切会把后缀吃进参数。
	rest := ""
	if i := strings.IndexAny(body, " \t\n\r"); i >= 0 {
		rest = strings.TrimSpace(body[i+1:])
	}
	restFields := strings.Fields(rest)
	arg := ""
	if len(restFields) > 0 {
		arg = restFields[0]
	}
	// /media 的检索词天然可能是多词（"/media 修复 老片"），整段才是完整意图。
	if cmd == CmdMedia {
		arg = rest
	}
	if requiresArg[cmd] && arg == "" {
		return nil, ErrMissingArgument
	}
	return &CommandRequest{Command: cmd, Arg: arg, Raw: rest}, nil
}

// HelpText 帮助文案（/help 与 /start 的回执）。
func HelpText() string {
	return strings.Join([]string{
		"可用命令：",
		"/start  — 开始使用",
		"/help   — 显示这份帮助",
		"/status <任务ID> — 查询任务状态",
		"/list   — 列出任务",
		"/add <下载链接> — 提交下载任务（多个链接用空格分隔即批量）",
		"直接发送一个或多个链接（无需命令）也能加入队列",
		"/cancel <任务ID> — 取消任务",
		"/media <检索词> — 检索媒体库",
	}, "\n")
}
