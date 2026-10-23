package store

// monitor.go：监控历史聚合（monitor_sample 表，docs/15 §3.2 迁移 0006）。
//
// sampler 每 60s 落一行聚合（均值），保留 7 天；history 视图按 range 取点。

// MonitorSampleRow 一行聚合样本。
type MonitorSampleRow struct {
	Ts     int64   `json:"ts"`
	CPU    float64 `json:"cpu"`
	Memory float64 `json:"memory"`
	Disk   float64 `json:"disk"`
	NetRx  float64 `json:"net_rx"`
	NetTx  float64 `json:"net_tx"`
}

// InsertMonitorSample UPSERT 一桶聚合（同 ts 覆盖，防重启后同分钟双写）。
func (s *Store) InsertMonitorSample(ts int64, cpu, memory, disk, netRx, netTx float64) error {
	_, err := s.RW.Exec(`INSERT INTO monitor_sample (ts, cpu, memory, disk, net_rx, net_tx)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(ts) DO UPDATE SET cpu=excluded.cpu, memory=excluded.memory,
		disk=excluded.disk, net_rx=excluded.net_rx, net_tx=excluded.net_tx`,
		ts, cpu, memory, disk, netRx, netTx)
	return err
}

// DeleteMonitorSamplesBefore 删除早于 ts 的样本（保留期回收）。
func (s *Store) DeleteMonitorSamplesBefore(ts int64) error {
	_, err := s.RW.Exec(`DELETE FROM monitor_sample WHERE ts < ?`, ts)
	return err
}

// MonitorHistory 取 [from, to] 区间样本（Unix 秒，按 ts 升序）。
func (s *Store) MonitorHistory(from, to int64) ([]MonitorSampleRow, error) {
	rows, err := s.RO.Query(`SELECT ts, cpu, memory, disk, net_rx, net_tx
		FROM monitor_sample WHERE ts >= ? AND ts <= ? ORDER BY ts ASC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MonitorSampleRow
	for rows.Next() {
		var r MonitorSampleRow
		if err := rows.Scan(&r.Ts, &r.CPU, &r.Memory, &r.Disk, &r.NetRx, &r.NetTx); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
