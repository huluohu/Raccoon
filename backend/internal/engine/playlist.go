package engine

// playlist.go：播放列表/合集解析（订阅发现内容用；docs/01 §3.2）。
//
// 真实调用 yt-dlp --flat-playlist --dump-single-json，取 entries[] 的
// id/title/url —— 订阅检查每次都真拉，不做模拟。

import (
	"context"
	"encoding/json"
	"strings"
)

// PlaylistEntry 播放列表中的一条。
type PlaylistEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// PlaylistResult 播放列表解析结果。
type PlaylistResult struct {
	Title    string          `json:"title"`
	Uploader string          `json:"uploader"`
	Entries  []PlaylistEntry `json:"entries"`
}

// ytdlpPlaylistJSON --flat-playlist 输出结构（只取关心的字段）。
type ytdlpPlaylistJSON struct {
	Title    string `json:"title"`
	Uploader string `json:"uploader"`
	Entries  []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		URL     string `json:"url"`
		Webpage string `json:"webpage_url"`
	} `json:"entries"`
}

// ResolvePlaylist 解析播放列表/合集/收藏夹（订阅发现内容）。
//
// proxy 为空 = 直连。这里必须支持代理：订阅的「周期发现」是订阅链路的**第一步**，
// 之前这一步恒直连 —— 只靠代理可达的站点，订阅根本发现不到内容，
// 也就永远不会建任务（而同一订阅在创建时的预览却是走代理的，前后口径不一致）。
func (y *YTDLP) ResolvePlaylist(ctx context.Context, url, proxy string) (*PlaylistResult, error) {
	args := []string{"--flat-playlist", "--dump-single-json", "--no-warnings", "--socket-timeout", "20"}
	args = append(args, y.impersonateArgs()...)
	if proxy != "" {
		args = append(args, "--proxy", proxy)
	}
	args = append(args, url)
	var raw strings.Builder
	err := RunCmd(ctx, y.Bin, args, func(l string) {
		raw.WriteString(l)
		raw.WriteByte('\n')
	}, "")
	if err != nil {
		return nil, classify(err, raw.String())
	}
	var d ytdlpPlaylistJSON
	if err := json.Unmarshal([]byte(raw.String()), &d); err != nil {
		for _, line := range reverseLines(raw.String()) {
			if !strings.HasPrefix(strings.TrimSpace(line), "{") {
				continue
			}
			if json.Unmarshal([]byte(line), &d) == nil {
				break
			}
		}
		if len(d.Entries) == 0 && d.Title == "" {
			return nil, &EngineError{Code: "PARSE_FAILED", Message: "播放列表解析失败: " + err.Error()}
		}
	}
	res := &PlaylistResult{Title: d.Title, Uploader: d.Uploader}
	for _, e := range d.Entries {
		entry := PlaylistEntry{ID: e.ID, Title: e.Title, URL: e.URL}
		if entry.URL == "" {
			entry.URL = e.Webpage
		}
		if entry.URL == "" {
			continue
		}
		res.Entries = append(res.Entries, entry)
	}
	return res, nil
}
