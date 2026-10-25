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
	"time"

	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/storage"
	"github.com/huluohu/raccoon/internal/store"
)

// setupFiles 注册媒体库路由。
func (s *Server) setupFiles() {
	s.router.GET("/api/v1/files", s.requireAuth(s.handleFilesList))
	s.router.GET("/api/v1/files/:id/download", s.requireAuth(s.handleFileDownload))
	s.router.GET("/api/v1/files/:id/preview", s.requireAuth(s.handleFilePreview))
	s.router.GET("/api/v1/files/:id/summary", s.requireAuth(s.handleFileSummaryGet))
	s.router.POST("/api/v1/files/:id/summary", s.requireAuth(s.csrfForSession(s.handleFileSummary)))
	s.router.POST("/api/v1/files/:id/convert", s.requireAuth(s.csrfForSession(s.handleFileConvert)))
	s.router.DELETE("/api/v1/files/:id", s.requireAuth(s.csrfForSession(s.handleFileDelete)))
}

// handleFileDelete 删除磁盘文件 + 索引（媒体库的「删文件」，与「删任务记录」分离）。
//
// 安全点：路径只从 DB 取，再用 storage.WithinRoot 校验，绝不信任前端传路径；
// 同时按 path 清掉 task_files 同名索引，否则任务详情里会留下指向已删文件的幽灵行。
func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request) error {
	f, err := s.store.GetMediaFile(param(r, "id"))
	if err != nil {
		return httpx.NotFound("文件不存在或已删除")
	}
	root := s.cfg.GetString("storage.data_dir")
	abs := f.Path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	abs = filepath.Clean(abs)
	if !storage.WithinAnyRoot(s.allowedFileRoots(), abs) {
		s.log.Warn("文件删除路径越界已拦截", map[string]any{"path": f.Path})
		return httpx.Forbidden("文件路径超出允许的存储目录范围")
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		s.log.Error("文件删除失败", map[string]any{"path": abs, "err": err.Error()})
		return httpx.Internal("文件删除失败：磁盘拒绝删除，请查看服务日志")
	}
	if err := s.store.DeleteMediaFile(f.ID); err != nil {
		return httpx.Internal("文件索引删除失败")
	}
	if _, err := s.store.DeleteTaskFilesByPath(f.Path); err != nil {
		s.log.Warn("产物索引同步删除失败", map[string]any{"path": f.Path, "err": err.Error()})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// handleFileConvert 按需二次格式转换 / 提取音频（POST /api/v1/files/:id/convert）。
// 支持 target="mp3"（提取音轨）或 target="mp4"（转码/remux 视频）。
func (s *Server) handleFileConvert(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Target string `json:"target"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	target := strings.ToLower(strings.TrimSpace(req.Target))
	if !engine.ConvertSupported(target) {
		return httpx.Validation("目标格式不支持，仅支持 mp3 或 mp4")
	}
	f, err := s.store.GetMediaFile(param(r, "id"))
	if err != nil {
		return fileErrToHTTP(err)
	}
	abs, err := s.resolveMediaPath(f.Path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return httpx.NotFound("源文件已不存在于磁盘")
	}

	ffmpeg := s.tasks.Engines().FFmpegBin
	if ffmpeg == "" {
		return httpx.Conflict("ffmpeg 未配置或不可用，无法执行格式转换")
	}

	dst, mode, err := engine.ConvertFile(r.Context(), ffmpeg, abs, target)
	if err != nil {
		s.log.Error("媒体文件转换失败", map[string]any{"id": f.ID, "target": target, "err": err.Error()})
		return httpx.Internal("转换失败: " + err.Error())
	}

	var size int64
	if st, err := os.Stat(dst); err == nil {
		size = st.Size()
	}
	name := filepath.Base(dst)
	mime := mimeByExt(name)
	kind := store.MediaKindOf(mime, name)

	now := time.Now().UnixMilli()
	newMedia := &store.MediaFile{
		ID:          store.NewTaskID(),
		TaskID:      f.TaskID,
		Path:        dst,
		Name:        name,
		Size:        size,
		Mime:        mime,
		Duration:    f.Duration,
		Platform:    f.Platform,
		Uploader:    f.Uploader,
		Kind:        kind,
		Previewable: kind == "video" || kind == "audio",
		CreatedAt:   now,
	}
	if err := s.store.UpsertMediaFile(newMedia); err != nil {
		s.log.Warn("转换产物入库失败", map[string]any{"path": dst, "err": err.Error()})
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"mode": mode,
		"file": newMedia,
	})
	return nil
}

// handleFileSummary 生成或重新生成媒体文件的 AI 总结（POST /api/v1/files/:id/summary）。
func (s *Server) handleFileSummary(w http.ResponseWriter, r *http.Request) error {
	f, err := s.store.GetMediaFile(param(r, "id"))
	if err != nil {
		return fileErrToHTTP(err)
	}
	if s.ai == nil {
		return httpx.Internal("AI 服务未初始化")
	}
	summary, err := s.ai.GenerateMediaSummary(r.Context(), f)
	if err != nil {
		s.log.Warn("AI 总结生成失败", map[string]any{"id": f.ID, "err": err.Error()})
		return httpx.Conflict("生成 AI 总结失败: " + err.Error())
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "summary": summary})
	return nil
}

// handleFileSummaryGet 读取已缓存的 AI 总结（GET /api/v1/files/:id/summary）。
func (s *Server) handleFileSummaryGet(w http.ResponseWriter, r *http.Request) error {
	f, err := s.store.GetMediaFile(param(r, "id"))
	if err != nil {
		return fileErrToHTTP(err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "summary": f.Summary})
	return nil
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

// allowedFileRoots 产物文件的合法根集合：数据根 + 独立下载根（当 download_dir
// 是绝对路径时，它脱离数据根；预览/下载/删除的越界校验必须把它算进来，
// 否则独立下载根里的文件会被误判 403）。
func (s *Server) allowedFileRoots() []string {
	roots := []string{s.cfg.GetString("storage.data_dir")}
	if dl := s.cfg.GetString("storage.download_dir"); filepath.IsAbs(dl) {
		roots = append(roots, filepath.Clean(dl))
	}
	return roots
}

// resolveMediaPath 解析媒体文件真实路径并校验在允许根集合内。
func (s *Server) resolveMediaPath(p string) (string, error) {
	if p == "" {
		return "", httpx.NotFound("文件路径为空")
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.cfg.GetString("storage.data_dir"), abs)
	}
	abs = filepath.Clean(abs)
	if !storage.WithinAnyRoot(s.allowedFileRoots(), abs) {
		s.log.Warn("媒体文件路径越界已拦截", map[string]any{"path": p})
		return "", httpx.Forbidden("文件路径超出允许的存储目录范围")
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
