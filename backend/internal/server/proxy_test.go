package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTestConnection_ProxyEmpty(t *testing.T) {
	s := &Server{}
	res := s.testConnection("proxy", map[string]any{"proxy": ""})
	if res.OK {
		t.Fatal("空代理地址不应测试通过")
	}
	if !strings.Contains(res.Message, "未配置") {
		t.Fatalf("空代理应提示未配置，实际：%s", res.Message)
	}
}

func TestTestConnection_ProxyInvalidScheme(t *testing.T) {
	s := &Server{}
	res := s.testConnection("proxy", map[string]any{"proxy": "ftp://127.0.0.1:1080"})
	if res.OK {
		t.Fatal("非法协议头不应测试通过")
	}
	if !strings.Contains(res.Message, "非法") {
		t.Fatalf("应提示协议非法，实际：%s", res.Message)
	}
}

func TestTestConnection_ProxyUnreachable(t *testing.T) {
	s := &Server{}
	// 用一个未开放的本地端口模拟无法连接的代理
	res := s.testConnection("proxy", map[string]any{"proxy": "http://127.0.0.1:54321"})
	if res.OK {
		t.Fatal("不可达代理不应测试通过")
	}
	if !strings.Contains(res.Message, "失败") && !strings.Contains(res.Message, "refused") && !strings.Contains(res.Message, "connect") {
		t.Fatalf("应包含失败信息，实际：%s", res.Message)
	}
}

func TestTestConnection_ProxySuccess(t *testing.T) {
	// 启动一个测试 HTTP 代理服务器：截获请求并返回 204
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxyServer.Close()

	s := &Server{}
	res := s.testConnection("proxy", map[string]any{"proxy": proxyServer.URL})
	if !res.OK {
		t.Fatalf("本地测试代理应返回成功，实际：%+v", res)
	}
	if !strings.Contains(res.Message, "代理可用") {
		t.Fatalf("文案应包含「代理可用」，实际：%s", res.Message)
	}
	if res.LatencyMS < 0 {
		t.Fatalf("延迟应非负，实际：%d", res.LatencyMS)
	}
}
