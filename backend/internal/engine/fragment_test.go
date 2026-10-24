package engine

// fragment_test.go：流分片识别。
//
// 判定必须精确：过宽会把正常文件名误伤（用户文件被当成碎片丢弃，
// 表现为「下载成功但媒体库里没有文件」）；过窄则碎片会被当成产物
// 收进媒体库（库里出现一堆 .f401.mp4 碎片，真合并文件反而缺失）。
//
// 判定基准是**合并产物名**（分片 = 合并产物 stem + ".f" + 数字 format_id），
// 不是对文件名做形状猜测。

import "testing"

func TestIsStreamFragment(t *testing.T) {
	merged := []string{"/dl/video [abc123].webm"}
	cases := []struct {
		path string
		want bool
	}{
		// 真分片（与合并产物同源）
		{"/dl/video [abc123].f401.mp4", true},
		{"/dl/video [abc123].f251.webm", true},

		// 正常文件名——不能被误伤
		{"/dl/video [abc123].mp4", false},
		{"/dl/my.file.name.mp4", false},
		{"/dl/video.f140.m4a", false}, // 与合并产物不同源
		{"/dl/other [xyz].f401.mp4", false},
		{"/dl/video [abc123].fabc.mp4", false}, // format_id 非数字
		{"/dl/video [abc123].f.mp4", false},    // format_id 为空
		{"", false},
	}
	for _, tc := range cases {
		if got := isStreamFragment(tc.path, merged); got != tc.want {
			t.Fatalf("isStreamFragment(%q) = %v，期望 %v", tc.path, got, tc.want)
		}
	}
}

// TestIsStreamFragment_无合并产物不判分片 没有合并产物时不存在分片概念。
func TestIsStreamFragment_无合并产物不判分片(t *testing.T) {
	if isStreamFragment("/dl/video [abc123].f401.mp4", nil) {
		t.Fatal("无合并产物时不应判为分片")
	}
}

// TestIsStreamFragment_多合并产物 一次下载多个条目时，合并产物有多个，
// 任一同源即判为分片。
func TestIsStreamFragment_多合并产物(t *testing.T) {
	merged := []string{
		"/dl/video A [aaa].webm",
		"/dl/video B [bbb].webm",
	}
	if !isStreamFragment("/dl/video B [bbb].f140.mp4", merged) {
		t.Fatal("应识别为第二个合并产物的分片")
	}
	if isStreamFragment("/dl/video C [ccc].f140.mp4", merged) {
		t.Fatal("不同源不应判为分片")
	}
}
