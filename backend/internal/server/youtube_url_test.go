package server

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/httpx"
)

// YouTube 链接的 video ID 必须 11 位。粘贴多一位是最常见输错形式，
// 放行到引擎后只会得到一句无信息量的「视频不可用」。
func TestValidateYouTubeID_长度异常给出可读提示(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://www.youtube.com/watch?v=5Qm0GPaOWM84", "12"},
		{"https://www.youtube.com/watch?v=abc", "3"},
		{"https://www.youtube.com/watch?v=", "缺少视频 ID"},
	}
	for _, c := range cases {
		err := validateYouTubeID(c.url)
		if err == nil {
			t.Fatalf("%s 应被拒绝，实际通过了", c.url)
		}
		var he *httpx.Error
		if !errors.As(err, &he) {
			t.Fatalf("%s 应返回 httpx.Error，实际 %T", c.url, err)
		}
		if he.HTTP != http.StatusBadRequest {
			t.Fatalf("%s 应返回 400，实际 %d", c.url, he.HTTP)
		}
		if !strings.Contains(he.Message, c.want) {
			t.Fatalf("%s 的提示应含 %q，实际 %q", c.url, c.want, he.Message)
		}
	}
}

func TestValidateYouTubeID_合法链接放行(t *testing.T) {
	for _, u := range []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com/watch?v=5Qm0GPaOWM8",
		"https://m.youtube.com/watch?v=5Qm0GPaOWM8&t=30s",
	} {
		if err := validateYouTubeID(u); err != nil {
			t.Fatalf("%s 应放行，实际 %v", u, err)
		}
	}
}

func TestValidateYouTubeID_非YouTube与其它形式不拦(t *testing.T) {
	for _, u := range []string{
		"https://www.bilibili.com/video/BV1xx411c7mD",
		"https://www.youtube.com/@laoming",
		"https://www.youtube.com/playlist?list=PL1234567890",
		"https://www.youtube.com/shorts/5Qm0GPaOWM8",
	} {
		if err := validateYouTubeID(u); err != nil {
			t.Fatalf("%s 不该被拦，实际 %v", u, err)
		}
	}
}
