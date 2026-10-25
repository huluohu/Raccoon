package ai

// summary.go：AI 智能视频总结与转写服务（兼容 OpenAI / DeepSeek / Ollama / LocalAI 接口格式）。
//
// 机制说明：
//   1. 优先提取同目录下伴生字幕（.srt/.vtt）或元数据（.info.json/.description）作为转录文本；
//   2. 若无字幕文件，根据视频标题、创作者、分段章节与任务事件流提取上下文；
//   3. 请求 OpenAI 兼容的 /chat/completions 接口生成精炼结构化总结；
//   4. 结果持久化入库并在详情页以 Markdown 格式高亮呈现。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/store"
)

// Summarizer AI 总结服务。
type Summarizer struct {
	cfg   *config.Config
	store *store.Store
}

func NewSummarizer(cfg *config.Config, st *store.Store) *Summarizer {
	return &Summarizer{cfg: cfg, store: st}
}

// GenerateMediaSummary 为指定媒体文件生成结构化 AI 总结。
func (s *Summarizer) GenerateMediaSummary(ctx context.Context, media *store.MediaFile) (string, error) {
	if media == nil {
		return "", errors.New("媒体对象为空")
	}

	endpoint := strings.TrimSpace(s.cfg.GetString("ai.endpoint"))
	apiKey := strings.TrimSpace(s.cfg.GetString("ai.api_key"))
	model := strings.TrimSpace(s.cfg.GetString("ai.model"))
	if model == "" {
		model = "deepseek-chat"
	}
	sysPrompt := strings.TrimSpace(s.cfg.GetString("ai.prompt"))
	if sysPrompt == "" {
		sysPrompt = "请用中文对以下视频/媒体内容进行精炼的结构化总结，包含核心内容概述与关键看点要点。"
	}

	if endpoint == "" {
		return "", errors.New("未配置 AI 服务端点，请在「设置」页配置 ai.endpoint")
	}

	// 收集文本素材（字幕 / 详细描述 / 基础元数据）
	contextText := collectMediaContext(media)

	// 调用 OpenAI 兼容接口
	summary, err := callChatCompletion(ctx, endpoint, apiKey, model, sysPrompt, contextText)
	if err != nil {
		return "", err
	}

	// 持久化到数据库
	_ = s.store.UpdateMediaSummary(media.ID, summary)
	if media.TaskID != "" {
		_ = s.store.UpdateTaskFields(media.TaskID, map[string]any{"summary": summary}, "")
	}

	return summary, nil
}

// collectMediaContext 从媒体文件周边收集最丰富的文本上下文。
func collectMediaContext(m *store.MediaFile) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("【标题】%s\n", m.Name))
	if m.Uploader != "" {
		sb.WriteString(fmt.Sprintf("【创作者】%s\n", m.Uploader))
	}
	if m.Platform != "" {
		sb.WriteString(fmt.Sprintf("【来源平台】%s\n", m.Platform))
	}

	dir := filepath.Dir(m.Path)
	base := strings.TrimSuffix(filepath.Base(m.Path), filepath.Ext(m.Path))

	// 尝试寻找伴生字幕文件 (.srt / .vtt)
	subContent := findSubtitleContent(dir, base)
	if subContent != "" {
		sb.WriteString("\n【视频字幕/台词文本】\n")
		// 截取前 8000 字符避免超长 token 限制
		if len([]rune(subContent)) > 8000 {
			sb.WriteString(string([]rune(subContent)[:8000]) + "…(后续文本略)")
		} else {
			sb.WriteString(subContent)
		}
		return sb.String()
	}

	// 尝试寻找 .description 或 .info.json
	descPath := filepath.Join(dir, base+".description")
	if descBytes, err := os.ReadFile(descPath); err == nil && len(descBytes) > 0 {
		sb.WriteString("\n【视频完整简介】\n")
		sb.Write(descBytes)
	}

	return sb.String()
}

// findSubtitleContent 在媒体同级目录下查找字幕文件并提取文本行。
func findSubtitleContent(dir, base string) string {
	candidates := []string{
		filepath.Join(dir, base+".srt"),
		filepath.Join(dir, base+".vtt"),
		filepath.Join(dir, base+".zh.srt"),
		filepath.Join(dir, base+".zh.vtt"),
		filepath.Join(dir, base+".zh-CN.vtt"),
		filepath.Join(dir, base+".zh-Hans.vtt"),
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil && len(data) > 0 {
			return cleanSubtitleText(string(data))
		}
	}
	return ""
}

// cleanSubtitleText 简单剥离 srt/vtt 的时间戳与序号，只留台词纯文本。
func cleanSubtitleText(raw string) string {
	var lines []string
	for _, l := range strings.Split(raw, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || l == "WEBVTT" || strings.HasPrefix(l, "NOTE") {
			continue
		}
		if strings.Contains(l, "-->") {
			continue // 时间轴行
		}
		// 跳过全数字序号行
		isNum := true
		for _, r := range l {
			if r < '0' || r > '9' {
				isNum = false
				break
			}
		}
		if isNum {
			continue
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n")
}

// callChatCompletion 调用 OpenAI 格式的 /chat/completions。
func callChatCompletion(ctx context.Context, endpoint, apiKey, model, sysPrompt, userContent string) (string, error) {
	u := strings.TrimRight(endpoint, "/")
	if !strings.HasSuffix(u, "/chat/completions") {
		u += "/chat/completions"
	}
	if _, err := url.Parse(u); err != nil {
		return "", fmt.Errorf("非法 AI 服务端点: %w", err)
	}

	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": sysPrompt},
			{"role": "user", "content": userContent},
		},
		"temperature": 0.5,
	}
	bodyJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 AI 接口失败: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI 服务返回异常 (HTTP %d): %s", resp.StatusCode, string(respBytes))
	}

	var res struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.Unmarshal(respBytes, &res); err != nil {
		return "", fmt.Errorf("解析 AI 响应失败: %w", err)
	}

	if res.Error != nil && res.Error.Message != "" {
		return "", fmt.Errorf("AI 错误: %s", res.Error.Message)
	}

	if len(res.Choices) == 0 || res.Choices[0].Message.Content == "" {
		return "", errors.New("AI 响应未包含内容")
	}

	return strings.TrimSpace(res.Choices[0].Message.Content), nil
}
