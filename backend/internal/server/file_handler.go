package server

// file_handler.go：媒体库路由（docs/04 §2.8）。
//
// 关键安全点：预览/下载都从 DB 取真实路径，再用 storage.WithinRoot 校验，
// 不信前端传的任何路径（防路径穿越）。

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/storage"
	"github.com/huluohu/raccoon/internal/store"
)

// setupFiles 注册媒体库路由。
func (s *Server) setupFiles() {
	s.router.GET("/api/v1/files", s.requireAuth(s.handleFilesList))
	s.router.GET("/api/v1/files/:id/download", s.requireAuth(s.handleFileDownload))
	s.router.GET("/api/v1/files/:id/preview", s.requireAuth(s.handleFilePreview))
}

// handleFilesList 媒体库列表（过滤 + 分页）。
func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	f := store.FileFilter{
		Platform: q.Get("platform"),
		Uploader: q.Get("uploader"),
		Kind:     q.Get("kind"),
		Query:    q.Get("q"),
		Limit:    atoiDefault(q.Get("limit"), 50),
	}
	offset := 0
	if s := q.Get("offset"); s != "" {
		offset, _ = strconv.Atoi(s)
	}
	f.Offset = offset
	items, total, err := s.store.ListMediaFiles(f)
	if err != nil {
		s.log.Error("媒体库查询失败", map[string]any{"err": err.Error()})
		return httpx.Internal("媒体库查询失败")
	}
	platforms, _ := s.store.ListMediaPlatforms()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"total":     total,
		"platforms": platforms,
	})
	return nil
}

// handleFileDownload 下载直链（Content-Disposition 附件）。
func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) error {
	f, err := s.store.GetMediaFile(param(r, "id"))
	if err != nil {
		return fileErrToHTTP(err)
	}
	abs, err := s.resolveMediaPath(f.Path)
	if err != nil {
		return err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return httpx.NotFound("文件已不存在")
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", urlEscape(f.Name)))
	http.ServeContent(w, r, f.Name, st.ModTime(), mustOpen(abs))
	return nil
}

// handleFilePreview 浏览器内预览：支持 Range 播放，不支持编码返回 415。
func (s *Server) handleFilePreview(w http.ResponseWriter, r *http.Request) error {
	f, err := s.store.GetMediaFile(param(r, "id"))
	if err != nil {
		return fileErrToHTTP(err)
	}
	if !f.Previewable {
		return &httpx.Error{HTTP: http.StatusUnsupportedMediaType, Code: "UNSUPPORTED_MEDIA",
			Message: "该文件编码浏览器无法直接播放，请下载后观看"}
	}
	abs, err := s.resolveMediaPath(f.Path)
	if err != nil {
		return err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return httpx.NotFound("文件已不存在")
	}
	mime := f.Mime
	if mime == "" {
		mime = mimeByExt(f.Name)
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, f.Name, st.ModTime(), mustOpen(abs))
	return nil
}

// resolveMediaPath 解析媒体文件真实路径并校验在数据根内。
func (s *Server) resolveMediaPath(p string) (string, error) {
	if p == "" {
		return "", httpx.NotFound("文件路径为空")
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.cfg.GetString("storage.data_dir"), abs)
	}
	abs = filepath.Clean(abs)
	if !storage.WithinRoot(s.cfg.GetString("storage.data_dir"), abs) {
		s.log.Warn("媒体文件路径越界已拦截", map[string]any{"path": p})
		return "", httpx.Forbidden("文件路径超出数据目录范围")
	}
	return abs, nil
}

// mimeByExt 扩展名 → MIME。
func mimeByExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".m4v":
		return "video/x-m4v"
	case ".mkv":
		return "video/x-matroska"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".ogg":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".flac":
		return "audio/flac"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".srt":
		return "application/x-subrip"
	case ".vtt":
		return "text/vtt"
	}
	return "application/octet-stream"
}

// urlEscape 文件名 URL 转义（Content-Disposition 用）。
func urlEscape(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~':
			b.WriteRune(r)
		default:
			b.WriteString(fmt.Sprintf("%%%02X", r))
		}
	}
	return b.String()
}

// mustOpen 打开文件（失败时返回只读空句柄，让 ServeContent 返回 500 而不是 panic）。
func mustOpen(path string) *os.File {
	f, err := os.Open(path)
	if err != nil {
		return os.NewFile(uintptr(0), path)
	}
	return f
}

// fileErrToHTTP store 错误 → HTTP 信封。
func fileErrToHTTP(err error) error {
	if err == store.ErrNoSuchFile {
		return httpx.NotFound("文件不存在")
	}
	return httpx.Internal("文件操作失败")
}
