package store

// counts.go：任务计数统计（总览页 / 队列状态 / 诊断包用）。

// CountTasks 总任务数（不含软删）。
func (s *Store) CountTasks() (int, error) {
	var n int
	err := s.RO.QueryRow(`SELECT COUNT(*) FROM tasks WHERE deleted_at IS NULL`).Scan(&n)
	return n, err
}

// CountTasksByStatus 各状态任务数（key=状态，value=数量；含 waiting/parsing/… 七态）。
func (s *Store) CountTasksByStatus() (map[string]int, error) {
	rows, err := s.RO.Query(`SELECT status, COUNT(*) FROM tasks WHERE deleted_at IS NULL GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{
		"waiting": 0, "parsing": 0, "downloading": 0, "post": 0,
		"done": 0, "failed": 0, "cancelled": 0,
	}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[status] = n
	}
	return out, rows.Err()
}

// CountActiveTasks 活跃态任务数（非终态）。
func (s *Store) CountActiveTasks() (int, error) {
	var n int
	err := s.RO.QueryRow(`SELECT COUNT(*) FROM tasks WHERE deleted_at IS NULL AND status IN ('waiting','parsing','downloading','post')`).Scan(&n)
	return n, err
}
