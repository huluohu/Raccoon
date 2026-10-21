// Package web 内嵌前端构建产物（web/dist）。CI 将前端构建输出拷贝到本目录后重新编译即完成托管（ADR-5）。
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist
var distFS embed.FS

// FS 返回去掉 dist 前缀的文件系统。
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// FileServer 返回静态文件服务（SPA fallback 由上层 server 处理）。
func FileServer() (http.Handler, error) {
	sub, err := FS()
	if err != nil {
		return nil, err
	}
	return http.FileServer(http.FS(sub)), nil
}

// IndexHTML 读取 SPA 入口（fallback 用）。
func IndexHTML() ([]byte, error) {
	return distFS.ReadFile("dist/index.html")
}
