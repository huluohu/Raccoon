// Package sse 提供 SSE 事件广播（docs/04 §3：进度即真相）。
// 事件即任务事件的广播投影——同一数据源，无第二状态。
package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/huluohu/raccoon/internal/logx"
)

// Event 一条广播事件（event + data JSON）。
type Event struct {
	Name string
	Data any
}

// Hub 订阅者注册中心（广播投影；慢消费者丢帧不阻塞生产者）。
type Hub struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
	log  *logx.Logger
}

// New 建 Hub。
func New(logger *logx.Logger) *Hub {
	if logger == nil {
		logger = logx.Default("sse")
	}
	return &Hub{subs: map[chan Event]struct{}{}, log: logger}
}

// Subscribe 订阅；返回事件通道与退订函数。缓冲 64，满则丢最旧（进度帧可丢，
// 状态帧不丢的策略简化处理：统一丢最旧，前端断连期用 GET /tasks 对账兜底）。
func (h *Hub) Subscribe() (chan Event, func()) {
	ch := make(chan Event, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// Broadcast 广播（非阻塞；慢消费者直接丢帧）。
func (h *Hub) Broadcast(name string, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- Event{Name: name, Data: data}:
		default:
			// 丢帧（前端下次全量对账兜底）。
		}
	}
}

// ServeHTTP SSE 长连接：心跳 25s、客户端断开即清理（docs/04 §3）。
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // 反代禁缓冲

	ch, unsub := h.Subscribe()
	defer unsub()

	flusher.Flush()
	hb := time.NewTicker(25 * time.Second)
	defer hb.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-ch:
			data, err := json.Marshal(ev.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, data)
			flusher.Flush()
		case <-hb.C:
			fmt.Fprintf(w, ": hb %d\n\n", time.Now().Unix())
			flusher.Flush()
		}
	}
}

// LastEventID 解析 Last-Event-ID 头（断线补发定位；本版仅读入，补发由 tasks 全量对账兜底）。
func LastEventID(r *http.Request) int64 {
	v := r.Header.Get("Last-Event-ID")
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
