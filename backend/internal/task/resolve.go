package task

// resolve.go：解析（预览 / 任务）共用的网络出口决策。
//
// 为什么要有这个文件：预览与下载是同一件事的两面——预览解析不出来，
// 下载必然也失败；预览能解析，下载就该能成。两条路径各写一套代理逻辑，
// 就会出现「明明能下载，却解析失败」的口径错位（实测踩过：任务走全局代理
// 下得好好的，预览却从直连开始，等直连超时才想起切代理——而那次的
// deadline 已经被直连耗尽，代理重试必然跟着一起失败）。
//
// 统一口径（docs/05 §7.4）：
//  1. 出口优先级：显式指定 > 任务级 proxy > 全局 net.proxy > 直连；
//  2. 主出口遇网络类异常（超时/重置/限流/人机拦截）→ 换备用出口重试一次
//     （代理挂了回退直连）；
//  3. 每次尝试都从父 ctx 派生**全新** ctx：上一次尝试可能已把 deadline 耗尽，
//     复用同一个 ctx 会让第二次尝试瞬间失败——这正是「能下载却解析失败」的成因。
//  4. 解析与下载必须用**同一个**出口：解析切到哪个出口，下载就跟着走哪个，
//     否则会出现「解析走代理成功、下载走直连超时」的错位。

import (
	"context"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/engine"
)

// resolveAttempts 出口尝试序列（按优先级）。
//
// explicit：调用方显式指定（预览接口的 proxy 参数、手工切代理重试）。
// taskProxy：任务级代理（tasks.proxy，任务自己的固定出口）。
// globalProxy：系统配置 net.proxy。
//
// 返回值第一项是主出口，第二项是备用出口（空串=直连）。
// 备用出口恒为直连：主出口是代理时，代理挂了还有直连可退；
// 主出口是直连（没配任何代理）时就没有备用出口可言。
func resolveAttempts(explicit, taskProxy, globalProxy string) (primary, fallback string) {
	global := strings.TrimSpace(globalProxy)
	primary = strings.TrimSpace(explicit)
	if primary == "" {
		primary = strings.TrimSpace(taskProxy)
	}
	if primary == "" {
		primary = global
	}
	if primary == "" {
		// 什么都没配：主出口直连，无备用。
		return "", ""
	}
	// 主出口已确定（有代理）。备用出口 = 直连。
	// 注意：当主出口**就是**全局代理时，备用仍是直连而非「全局代理」——
	// 否则备用等于主出口，重试没有意义。
	return primary, ""
}

// resolveWithProxy 按出口序列解析 URL，返回解析结果与实际使用的出口。
//
// 返回：(结果, 实际使用的出口, 切换说明, 错误)。
// 切换说明非空表示中途换过出口（供界面如实告知用户）。
//
// attemptTimeout > 0 时，每次尝试各自拥有该时限（且从父 ctx 派生全新 ctx）；
// = 0 时每次尝试直接用父 ctx（跟随调用方的整体 deadline）。
func (m *Manager) resolveWithProxy(
	parent context.Context,
	eng engine.Engine,
	url, cookieFile, primary, fallback string,
	attemptTimeout time.Duration,
) (*engine.ResolveResult, string, string, error) {
	chain := []string{primary}
	if primary != fallback {
		chain = append(chain, fallback)
	}

	var lastErr error
	usedProxy := primary
	for i, p := range chain {
		// 每次尝试一个全新 ctx：上一次尝试的 deadline 可能已耗尽。
		attemptCtx := parent
		var cancel context.CancelFunc
		if attemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(parent, attemptTimeout)
		}

		res, err := eng.Resolve(attemptCtx, url, engine.ResolveOpts{Cookies: cookieFile, Proxy: p})
		if cancel != nil {
			cancel()
		}
		if err == nil {
			note := ""
			if i > 0 {
				note = switchNote(p, chain[i-1])
				usedProxy = p
			}
			return res, usedProxy, note, nil
		}
		lastErr = err
		// 只有网络类异常才值得换出口重试：平台类错误（404/需登录）换出口也没用。
		if !IsNetworkError(err) {
			break
		}
	}
	return nil, usedProxy, "", lastErr
}

// switchNote 生成出口切换说明（给界面看的实话）。
func switchNote(to, from string) string {
	toDesc, fromDesc := exitDesc(to), exitDesc(from)
	return fromDesc + "异常 → 已切换" + toDesc
}

// exitDesc 出口的人类可读描述。
func exitDesc(proxy string) string {
	if strings.TrimSpace(proxy) == "" {
		return "直连"
	}
	return "代理"
}
