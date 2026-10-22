package server

// integration_adapter.go：外部客户端 → task 通道接口的适配层。
//
// 为什么要这一层而不是让 task 直接依赖 qb / n115 包：
//   - task 包是领域核心，不应依赖具体外部服务包（依赖方向：server → task → 抽象接口，
//     反向依赖会让 task 变成大杂烩，也不好单测）；
//   - qb / n115 包的类型（AddOptions / OfflineTask）与任务域的类型语义不同，
//     直接传会丢失字段（比如 qB 的 Tags、115 的 Files）。
//
// 适配层做纯类型转换，无业务逻辑。

import (
	"context"

	"github.com/huluohu/raccoon/internal/n115"
	"github.com/huluohu/raccoon/internal/qb"
	"github.com/huluohu/raccoon/internal/task"
)

// qbAdapter 把 *qb.Client 适配成 task.QBClient。
type qbAdapter struct {
	c *qb.Client
}

// NewQBAdapter 构造适配器。
func NewQBAdapter(c *qb.Client) task.QBClient {
	if c == nil {
		return nil
	}
	return &qbAdapter{c: c}
}

func (a *qbAdapter) AddMagnet(ctx context.Context, magnet string, opts task.QBAddOptions) (string, error) {
	return a.c.AddMagnet(ctx, magnet, qb.AddOptions{
		SavePath:  opts.SavePath,
		Category:  opts.Category,
		Tags:      opts.Tags,
		Paused:    opts.Paused,
		SkipChecking: opts.SkipCheck,
	})
}

func (a *qbAdapter) Torrents(ctx context.Context, hashes []string) ([]task.QBTorrent, error) {
	ts, err := a.c.Torrents(ctx, hashes)
	if err != nil {
		return nil, err
	}
	out := make([]task.QBTorrent, 0, len(ts))
	for _, t := range ts {
		// qB 的 progress 是 0..1，Percent 已是 0..100。
		downloaded := t.Size - int64(float64(t.Size)*(1-t.Progress/100))
		if downloaded < 0 {
			downloaded = 0
		}
		out = append(out, task.QBTorrent{
			Hash:       t.Hash,
			State:      t.State,
			Percent:    t.Percent,
			Size:       t.Size,
			SavePath:   t.SavePath,
			Name:       t.Name,
			Downloaded: downloaded,
		})
	}
	return out, nil
}

func (a *qbAdapter) Delete(ctx context.Context, hashes []string, deleteFiles bool) error {
	return a.c.Delete(ctx, hashes, deleteFiles)
}

// n115Adapter 把 *n115.Client 适配成 task.N115Client。
type n115Adapter struct {
	c *n115.Client
}

// NewN115Adapter 构造适配器。
func NewN115Adapter(c *n115.Client) task.N115Client {
	if c == nil {
		return nil
	}
	return &n115Adapter{c: c}
}

func (a *n115Adapter) SubmitOffline(ctx context.Context, req task.N115Request) (*task.N115Task, error) {
	t, err := a.c.SubmitOffline(ctx, n115.OfflineRequest{
		URL:      req.URL,
		WPPath:   req.WPPath,
		WPPathID: req.WPPathID,
		CID:      req.CID,
	})
	if err != nil {
		return nil, err
	}
	return n115TaskToTask(t), nil
}

func (a *n115Adapter) GetOfflineTask(ctx context.Context, ref string) (*task.N115Task, error) {
	t, err := a.c.GetOfflineTask(ctx, ref)
	if err != nil {
		return nil, err
	}
	return n115TaskToTask(t), nil
}

// n115TaskToTask 转换 115 任务快照。
func n115TaskToTask(t *n115.OfflineTask) *task.N115Task {
	if t == nil {
		return nil
	}
	out := &task.N115Task{
		OfflineID: t.OfflineID,
		TaskID:    t.TaskID,
		Status:    string(t.Status),
		Progress:  t.Progress,
		Size:      t.Size,
		Name:      t.Name,
	}
	for _, f := range t.Files {
		out.Files = append(out.Files, task.N115File{
			FileID: f.FileID,
			Name:   f.Name,
			Size:   f.Size,
			IsDir:  f.IsDir,
		})
	}
	return out
}
