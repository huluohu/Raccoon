package task

// notify.go：任务事件 → 通知中心的钩子（docs/02 §2 依赖方向：notify 不依赖 task）。
//
// task 包只定义 Notifier 接口并调用，不反向依赖 server/notify 实现。

// Notifier 通知出口（由上层注入）。
type Notifier interface {
	// Notify 写一条通知并广播。
	Notify(kind, severity, title, body, taskID string)
}

// SetNotifier 注入通知出口（server 在 New 时设置）。
func (m *Manager) SetNotifier(n Notifier) {
	m.mu.Lock()
	m.notifier = n
	m.mu.Unlock()
}

// notify 内部调用（无通知器时静默跳过）。
func (m *Manager) notify(kind, severity, title, body, taskID string) {
	m.mu.Lock()
	n := m.notifier
	m.mu.Unlock()
	if n == nil {
		return
	}
	n.Notify(kind, severity, title, body, taskID)
}

// Notify 供其他服务（如直播守护）广播通知用的导出入口：
// kind 语义与通知中心一致（live_start / disk_warn / system…）。
func (m *Manager) Notify(kind, severity, title, body, taskID string) {
	m.notify(kind, severity, title, body, taskID)
}
