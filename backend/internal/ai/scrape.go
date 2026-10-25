package ai

// scrape.go：AI 元数据刮削（从字幕/简介提取结构化信息）。
//
// 与「AI 总结」的区别：
//   - 总结（summary.go）：一段话，讲清「这内容讲了什么」；
//   - 刮削（scrape.go）：结构化字段，讲清「这条内容叫什么、谁做的、属于什么标签」——
//     对应影视站的「刮削」（从片源自动补齐标题/简介/标签/分类）。
//
// 为什么用 AI 而不是接 TMDB 等刮削站：
//   - 自托管用户常看的是各平台内容，不是影视库条目，接 TMDB 对不上；
//   - 字幕里已经包含了标题、简介、章节、标签等线索，AI 直接读字幕更准；
//   - 已有 OpenAI 兼容端点配置即可用，不新增外部依赖与密钥。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/huluohu/raccoon/internal/config"
)

// ScrapeResult 刮削出的结构化元数据。
type ScrapeResult struct {
	Title       string   `json:"title"`
	Uploader    string   `json:"uploader"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Summary     string   `json:"summary"`
}

// ScrapeMedia 用 AI 从媒体文件的字幕/简介刮削结构化元数据。
func ScrapeMedia(ctx context.Context, cfg *config.Config, mediaPath, fallbackTitle string) (*ScrapeResult, error) {
	endpoint := strings.TrimSpace(cfg.GetString("ai.endpoint"))
	if endpoint == "" {
		return nil, errors.New("未配置 AI 服务端点，无法刮削元数据")
	}
	apiKey := strings.TrimSpace(cfg.GetString("ai.api_key"))
	model := strings.TrimSpace(cfg.GetString("ai.model"))
	if model == "" {
		model = "deepseek-chat"
	}

	content := collectScrapeContext(mediaPath)
	if content == "" {
		return nil, errors.New("该文件没有可用的字幕或简介，无法自动刮削（可手动编辑元数据）")
	}

	sysPrompt := `你是媒体元数据刮削助手。请阅读用户提供的字幕/简介文本，提取结构化信息，只输出 JSON（不要任何解释文字或代码块标记）：
{
  "title": "准确的标题（没有明确标题就用原标题）",
  "uploader": "作者/UP主/频道名（无法确定就留空字符串）",
  "description": "2-4 句的简介，说明这是什么内容、讲什么",
  "tags": ["标签1", "标签2", "标签3"],
  "summary": "一段话的核心内容总结"
}
tags 给 3-6 个中文标签。`

	userContent := fmt.Sprintf("原标题：%s\n\n以下是该媒体的字幕/简介文本：\n%s", fallbackTitle, content)

	raw, err := callChatCompletion(ctx, endpoint, apiKey, model, sysPrompt, userContent)
	if err != nil {
		return nil, err
	}
	return parseScrapeJSON(raw)
}

// parseScrapeJSON 解析 AI 返回的 JSON（容忍 ```json 包裹与前后废话）。
func parseScrapeJSON(raw string) (*ScrapeResult, error) {
	s := strings.TrimSpace(raw)
	// 去掉 ```json ... ``` 包裹
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	// 截取最外层 JSON 对象
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var out ScrapeResult
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("AI 返回内容不是合法 JSON: %w", err)
	}
	if strings.TrimSpace(out.Title) == "" {
		return nil, errors.New("AI 未返回标题")
	}
	return &out, nil
}

// collectScrapeContext 收集字幕/简介文本（作为刮削素材）。
func collectScrapeContext(mediaPath string) string {
	dir := filepath.Dir(mediaPath)
	base := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))

	var sb strings.Builder
	// 字幕优先（信息量最大）
	for _, cand := range []string{".srt", ".vtt", ".zh.srt", ".zh.vtt", ".ass"} {
		data, err := os.ReadFile(filepath.Join(dir, base+cand))
		if err == nil && len(data) > 0 {
			sb.WriteString(cleanSubtitleText(string(data)))
			break
		}
	}
	// 简介
	for _, cand := range []string{".description", ".info.json"} {
		data, err := os.ReadFile(filepath.Join(dir, base+cand))
		if err == nil && len(data) > 0 {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(string(data))
			break
		}
	}
	text := strings.TrimSpace(sb.String())
	// 超长截断（控制 token 成本）
	if len([]rune(text)) > 12000 {
		text = string([]rune(text)[:12000]) + "\n…（后续内容已截断）"
	}
	return text
}
