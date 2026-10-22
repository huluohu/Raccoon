package tgbot

// dedup.go：入站 update_id 去重。
//
// 为什么需要去重：Telegram 的 getUpdates 在网络抖动时可能重投同一条 update
// （服务端认为客户端没确认）。若上层把同一条 /add 处理两次，就会重复提交
// 任务——用户看到两条「已提交」，队列里也多一个任务。

// seenSet 固定容量的 update_id 去重表（环形缓冲 + 索引 map）。
//
// 用环形缓冲而不是无界 map：update_id 单调递增，只要记住最近 N 个就能挡住
// 绝大多数重投（重投总是紧邻原 update）。容量固定 → 内存占用有上界，
// 不会因为长期运行而无限增长。
//
// 配一个 map 做索引：查重是 O(1)，不用每次线性扫整个窗口。
type seenSet struct {
	// buf 环形缓冲，存最近 capacity 个 update_id。
	buf []int64
	// index 是 update_id → 在 buf 中的下标，用于 O(1) 查重与淘汰。
	index map[int64]int
	// next 下一个写入位置。
	next int
	// filled 已写入的元素个数。
	filled int
}

// newSeenSet 构造去重表；capacity<=0 时用默认值 1024。
func newSeenSet(capacity int) *seenSet {
	if capacity <= 0 {
		capacity = 1024
	}
	return &seenSet{
		buf:   make([]int64, capacity),
		index: make(map[int64]int, capacity),
	}
}

// Seen 判断 update_id 是否已出现过，并把它记入窗口。
//
// 合并「查 + 记」成一个操作是有意的：分两步做的话，两个并发处理同一
// update 时可能都查到「没见过」，去重就失效了。调用方在各自的 goroutine
// 里处理 update 时必须用互斥保护本方法。
func (s *seenSet) Seen(updateID int64) bool {
	if _, ok := s.index[updateID]; ok {
		return true
	}
	// 写满一圈时，槽位里那条旧记录要一起从索引里删掉，否则索引会无限增长
	// （这正是环形缓冲的意义：内存有上界）。
	if s.filled == len(s.buf) {
		delete(s.index, s.buf[s.next])
	} else {
		s.filled++
	}
	s.buf[s.next] = updateID
	s.index[updateID] = s.next
	s.next = (s.next + 1) % len(s.buf)
	return false
}

// Len 返回当前窗口内的元素个数（测试用）。
func (s *seenSet) Len() int { return s.filled }
