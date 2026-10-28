package server

// thumbnail_handler.go：任务缩略图代理（下载 → 去黑边 → 缩放 → 缓存）。
//
// 为什么不让前端直连平台 CDN：
// 1) **黑边**：平台给的封面常把 2.35:1 画面垫在 16:9 画布里（上下黑边），
//    CSS 裁不掉「画布里自带的黑边」，列表里的小图看着像画面没适配
//    （2026-10-28 用户截图）。
// 2) **防盗链/过期**：部分平台的图片 URL 带签名或校验 Referer，直连会 403。
// 3) **重复回源**：每个客户端都要去平台拉一次，服务端缓存一次大家都省。
//
// 失败时**重定向回原始直链**而不是报错：客户端自己能拉到就还能显示
// （服务端网络不通不代表客户端网络不通）。

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/httpx"
)

// handleTaskThumbnail 任务缩略图（GET /api/v1/tasks/:id/thumbnail）。
func (s *Server) handleTaskThumbnail(w http.ResponseWriter, r *http.Request) error {
	t, err := s.store.GetTask(param(r, "id"))
	if err != nil {
		return taskErrToHTTP(err)
	}
	src := strings.TrimSpace(t.Thumbnail)
	if src == "" {
		return httpx.NotFound("该任务没有缩略图")
	}

	cachePath := s.thumbnailCachePath(t.ID, src)
	if st, err := os.Stat(cachePath); err == nil && st.Size() > 0 {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, cachePath)
		return nil
	}

	// 下载原图（走与全局一致的出口）。
	raw, err := s.downloadThumbnail(r.Context(), src)
	if err != nil {
		// 服务端拉不到 → 让浏览器自己去拉（它可能有不同的出口）。
		http.Redirect(w, r, src, http.StatusFound)
		return nil
	}

	// 落临时文件 → 去黑边 + 缩放 → 原子改名进缓存。
	tmp, err := os.CreateTemp(filepath.Dir(cachePath), "thumb-*.jpg")
	if err != nil {
		http.Redirect(w, r, src, http.StatusFound)
		return nil
	}
	tmpName := tmp.Name()
	_, _ = tmp.Write(raw)
	_ = tmp.Close()
	defer os.Remove(tmpName) // 成功改名后这里删不到文件

	out := tmpName
	ffmpeg := s.tasks.Engines().FFmpegBin
	if ffmpeg != "" {
		processed := tmpName + ".p.jpg"
		if err := engine.TrimLetterbox(r.Context(), ffmpeg, s.tasks.Engines().FFprobeBin, tmpName, processed, 640); err == nil {
			out = processed
			defer os.Remove(processed)
		}
	}
	if err := os.Rename(out, cachePath); err != nil {
		http.Redirect(w, r, src, http.StatusFound)
		return nil
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, cachePath)
	return nil
}

// thumbnailCachePath 缓存文件路径：任务 id + 缩略图 URL 摘要
// （URL 变了就换新文件名，旧的自然过期）。
func (s *Server) thumbnailCachePath(taskID, src string) string {
	sum := sha1.Sum([]byte(src))
	dir := filepath.Join(s.cfg.GetString("storage.data_dir"), "cache", "thumbs")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, taskID+"-"+hex.EncodeToString(sum[:])[:8]+".jpg")
}

// downloadThumbnail 下载平台缩略图（走 net.proxy 出口；没配代理则直连）。
func (s *Server) downloadThumbnail(ctx context.Context, src string) ([]byte, error) {
	u, err := url.Parse(src)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, os.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; raccoon/1.0)")
	resp, err := s.thumbClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, os.ErrNotExist
	}
	// 最多 8MB：缩略图不该有这么大，超了当异常处理。
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// thumbClient 出站客户端：配了代理就走代理（与解析/下载同一出口口径）。
func (s *Server) thumbClient() *http.Client {
	raw := strings.TrimSpace(s.cfg.GetString("net.proxy"))
	if raw == "" {
		return &http.Client{Timeout: 20 * time.Second}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return &http.Client{Timeout: 20 * time.Second}
	}
	// Go 的 net/http 认 socks5（不认 socks5h：它没有「让代理解析域名」的开关），
	// 这里把 socks5h 降级成 socks5，其余原样。
	if u.Scheme == "socks5h" {
		u.Scheme = "socks5"
	}
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		Timeout:   20 * time.Second,
	}
}
