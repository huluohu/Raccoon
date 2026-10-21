// Package server 提供 HTTP 服务：轻量 router + 中间件链 + 路由注册（ADR-1：标准库自建，零框架依赖）。
package server

import (
	"net/http"
	"strings"
)

// HandlerFunc 返回 error 的处理器；错误经中间件/统一出口转错误信封。
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Middleware 包裹处理器（日志/恢复/RequestID/限流在此挂载）。
type Middleware func(HandlerFunc) HandlerFunc

type seg struct {
	literal string
	param   string // ":id"
}

type route struct {
	method  string
	segs    []seg
	handler HandlerFunc
}

// Router 仅支持「/a/:param/b」一段参数与「/* 」尾部通配（SPA fallback 用）。
type Router struct {
	mws      []Middleware
	routes   []*route
	fallback HandlerFunc
}

func NewRouter(mws ...Middleware) *Router {
	return &Router{mws: mws}
}

// Handle 注册路由；pattern 形如 "/api/v1/tasks/:id/cancel"。
func (rt *Router) Handle(method, pattern string, h HandlerFunc) {
	var segs []seg
	for _, part := range strings.Split(strings.Trim(pattern, "/"), "/") {
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, ":") {
			segs = append(segs, seg{param: part[1:]})
		} else {
			segs = append(segs, seg{literal: part})
		}
	}
	rt.routes = append(rt.routes, &route{method: method, segs: segs, handler: h})
}

func (rt *Router) GET(pattern string, h HandlerFunc)    { rt.Handle(http.MethodGet, pattern, h) }
func (rt *Router) POST(pattern string, h HandlerFunc)   { rt.Handle(http.MethodPost, pattern, h) }
func (rt *Router) PUT(pattern string, h HandlerFunc)    { rt.Handle(http.MethodPut, pattern, h) }
func (rt *Router) DELETE(pattern string, h HandlerFunc) { rt.Handle(http.MethodDelete, pattern, h) }

// Fallback 注册未匹配路径的处理（SPA 静态托管）。
func (rt *Router) Fallback(h HandlerFunc) { rt.fallback = h }

// Params 存放 URL 路径参数（中间件写入 request context）。

// WithParams 注入路径参数；handler 用 Params(r) 读取。
func WithParams(r *http.Request, params map[string]string) *http.Request {
	return r.WithContext(withParams(r, params))
}

func Params(r *http.Request) map[string]string {
	if p, ok := paramsFrom(r); ok {
		return p
	}
	return nil
}

func (rt *Router) chain(h HandlerFunc) http.HandlerFunc {
	for i := len(rt.mws) - 1; i >= 0; i-- {
		h = rt.mws[i](h)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			handleErr(w, r, err)
		}
	}
}

// handleErr 兜底错误出口（由 Server 注入统一写法，避免循环依赖）。
var handleErr = func(w http.ResponseWriter, r *http.Request, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// SetErrorHandler 由 server 初始化时替换为 httpx.WriteErr。
func (rt *Router) SetErrorHandler(f func(w http.ResponseWriter, r *http.Request, err error)) {
	handleErr = f
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	var parts []string
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	for _, rr := range rt.routes {
		if rr.method != r.Method {
			continue
		}
		if params, ok := match(rr.segs, parts); ok {
			rt.chain(rr.handler)(w, WithParams(r, params))
			return
		}
	}
	if rt.fallback != nil {
		rt.chain(rt.fallback)(w, r)
		return
	}
	http.NotFound(w, r)
}

func match(segs []seg, parts []string) (map[string]string, bool) {
	if len(segs) != len(parts) {
		return nil, false
	}
	params := map[string]string{}
	for i, s := range segs {
		if s.param != "" {
			params[s.param] = parts[i]
			continue
		}
		if s.literal != parts[i] {
			return nil, false
		}
	}
	return params, true
}
