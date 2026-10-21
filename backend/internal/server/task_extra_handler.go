package server

// task_extra_handler.go：任务域补充端点（docs/04 §2.2 批量/文件 + §5 父子任务）。
//
// - POST /tasks/batch：批量取消/重试/删除，逐个报告（单条失败不影响其余）。
// - GET  /tasks/{id}/files：产物列表。
// - DELETE /tasks/{id}/files/{fileId}：删实际文件（服务端二次校验路径在数据根内）。
// - POST /tasks/parent：批量（播放列表/频道）建父子任务，父聚合子进度。

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/storage"
	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/task"
)

// handleTasksBatch 批量操作（docs/04 §2.2）。
func (s *Server) handleTasksBatch(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	switch req.Action {
	case "cancel", "retry", "delete":
	default:
		return httpx.Validation("action 需为 cancel / retry / delete")
	}
	if len(req.IDs) == 0 {
		return httpx.Validation("ids 不能为空")
	}
	if len(req.IDs) > 200 {
		return httpx.Validation("单次最多批量操作 200 个任务")
	}
	succeeded := []string{}
	failed := []map[string]string{}
	for _, id := range req.IDs {
		var err error
		switch req.Action {
		case "cancel":
			err = s.tasks.Cancel(id)
		case "retry":
			err = s.tasks.Retry(id)
		case "delete":
			err = s.store.SoftDelete(id)
		}
		if err != nil {
			failed = append(failed, map[string]string{"id": id, "reason": err.Error()})
			continue
		}
		succeeded = append(succeeded, id)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"succeeded": succeeded,
		"failed":    failed,
	})
	return nil
}

// handleTaskFiles 任务产物列表。
func (s *Server) handleTaskFiles(w http.ResponseWriter, r *http.Request) error {
	id := param(r, "id")
	if _, err := s.store.GetTask(id); err != nil {
		return taskErrToHTTP(err)
	}
	files, err := s.store.TaskFiles(id)
	if err != nil {
		return httpx.Internal("产物查询失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": files})
	return nil
}

// handleTaskFileDelete 删除实际文件（独立危险操作；路径服务端重新校验）。
func (s *Server) handleTaskFileDelete(w http.ResponseWriter, r *http.Request) error {
	taskID := param(r, "id")
	fileID := param(r, "fileId")
	if _, err := s.store.GetTask(taskID); err != nil {
		return taskErrToHTTP(err)
	}
	files, err := s.store.TaskFiles(taskID)
	if err != nil {
		return httpx.Internal("产物查询失败")
	}
	var target *store.TaskFile
	for i := range files {
		if files[i].ID == fileID {
			target = files[i]
			break
		}
	}
	if target == nil {
		return httpx.NotFound("文件不存在")
	}
	// 路径二次校验：必须仍在数据根内（不信前端传的路径）。
	root := s.cfg.GetString("storage.data_dir")
	abs := target.Path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	abs = filepath.Clean(abs)
	if !storage.WithinRoot(root, abs) {
		s.log.Warn("文件删除路径越界已拦截", map[string]any{"path": target.Path})
		return httpx.Forbidden("文件路径超出数据目录范围")
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return httpx.Internal("文件删除失败: " + err.Error())
	}
	if err := s.store.DeleteTaskFile(fileID); err != nil {
		return httpx.Internal("文件索引删除失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// handleTaskParentCreate 批量任务（父子）：父任务 + 子任务树。
//
// 父进度 = Σ子 downloaded_size / Σ子 expected_size（任一子大小未知 → 显示「N/M 项」）。
// 父终态：全部子终态 → done；存在失败子 → done 且带 failed_children=N。
func (s *Server) handleTaskParentCreate(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Items   []submitItemJSON `json:"items"`
		Quality string           `json:"quality"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if len(req.Items) == 0 {
		return httpx.Validation("items 不能为空")
	}
	if len(req.Items) > 200 {
		return httpx.Validation("单次最多创建 200 个子任务")
	}
	for _, it := range req.Items {
		if err := validateOutboundURL(it.URL); err != nil {
			return err
		}
	}
	if err := s.tasks.Engines().YTDLP.Available(); err != nil {
		return &httpx.Error{HTTP: http.StatusServiceUnavailable, Code: httpx.CodeNoEngine, Message: err.Error()}
	}
	// 建父任务（kind=download，url=来源列表首项作占位；子任务 parent_id 指向它）。
	now := nowMillis()
	parent := &store.Task{
		Kind:          task.KindDownload,
		Source:        task.SourceWeb,
		Platform:      "batch",
		URL:           req.Items[0].URL,
		DedupeKey:     store.DedupeKey(task.KindDownload, "batch", batchDedupeKey(req.Items)),
		Status:        task.StatusWaiting,
		Quality:       req.Quality,
		ConfigVersion: s.cfg.Version(),
		MaxAttempts:   3,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.store.CreateTask(parent); err != nil {
		var de *store.ErrDedupeActive
		if errorsAs(err, &de) {
			return httpx.Conflict("相同的批量任务已在队列中")
		}
		return httpx.Internal("父任务创建失败")
	}
	_ = s.store.AppendEvent(parent.ID, "info", "created", "批量任务已创建", "")
	// 建子任务。
	created := 0
	children := []*store.Task{}
	for _, it := range req.Items {
		pid := parent.ID
		child := &store.Task{
			ParentID:      &pid,
			Kind:          task.KindDownload,
			Source:        task.SourceWeb,
			SourceRef:     parent.ID,
			URL:           strings.TrimSpace(it.URL),
			DedupeKey:     store.DedupeKey(task.KindDownload, "", it.URL),
			Status:        task.StatusWaiting,
			Quality:       it.Quality,
			SavePath:      it.SavePath,
			ConfigVersion: s.cfg.Version(),
			MaxAttempts:   3,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := s.store.CreateTask(child); err != nil {
			continue // 重复项跳过
		}
		_ = s.store.AppendEvent(child.ID, "info", "created", "批量子任务", "")
		children = append(children, child)
		created++
	}
	s.hub.Broadcast("task.status", map[string]any{"task_id": parent.ID, "status": "waiting"})
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"data":         parent,
		"children":     children,
		"child_total":  created,
		"child_failed": 0,
	})
	return nil
}

// batchDedupeKey 批量任务去重键（成员 URL 集合哈希）。
func batchDedupeKey(items []submitItemJSON) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString(strings.TrimSpace(it.URL))
		b.WriteString("|")
	}
	return store.DedupeKey("batch", "batch", b.String())
}
