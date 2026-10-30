package task

// exit_used_test.go：重试时的「自动切换代理」判定与记录。
//
// 2026-10-30 修的缺陷：重试分支只看 tasks.proxy（**用户配置的**出口）就断言
// 「已自动切换代理」。但出口决策是「显式 > 任务级 > 全局 net.proxy > 直连」——
// 任务没配 proxy 时全局代理**本来就在用**，于是：
//
//	· 所谓的「切换」把 proxy 字段写成同一个全局代理 = 出口根本没变；
//	· 日志却打印「已自动切换代理」——**误导排障**（用户以为出口变了，
//	  去查「代理规则没覆盖」，而问题根本不在那里）。
//
// 修复后按**事实**判定：本轮实际用的出口（exitUsed）为空串=直连时，
// 切到全局代理才是真切换；本轮已走代理时如实说明「无其它代理可切换」。

import (
	"strings"
	"testing"
)

// 判定逻辑本体在 worker.go 的 decideProxySwitch（纯函数，这里直接调用）。

// retryDecision 重试分支里「要不要切代理」的判定结果。
type retryDecision struct {
	// switchTo 非空 = 真的换出口了（值=新出口）。
	switchTo string
	// message 给用户看的话术。
	message string
}

// Test判定_本轮已走全局代理时不得声称切换 这是本次修复的核心断言：
// 任务没配 proxy、全局配了代理 → 本轮实际走的就是全局代理，
// 此时**不能再声称「已自动切换代理」**（出口根本没变）。
func Test判定_本轮已走全局代理时不得声称切换(t *testing.T) {
	// exitUsed = 全局代理地址（本轮实际用的）
	switchTo, message := decideProxySwitch("http://127.0.0.1:7890", "", "http://127.0.0.1:7890", true)
	d := retryDecision{switchTo: switchTo, message: message}
	if d.switchTo != "" {
		t.Errorf("本轮已走全局代理，不应再切（出口没变），却切到了 %q", d.switchTo)
	}
	if strings.Contains(d.message, "已自动切换代理") {
		t.Errorf("不得声称「已自动切换代理」，实际话术 %q", d.message)
	}
	if !strings.Contains(d.message, "已走代理") {
		t.Errorf("话术应如实说明本轮已走代理，实际 %q", d.message)
	}
}

// Test判定_本轮走直连且配了全局代理时真切换 直连失败 + 有全局代理 → 真切换。
func Test判定_本轮走直连且配了全局代理时真切换(t *testing.T) {
	switchTo, message := decideProxySwitch("", "", "http://127.0.0.1:7890", true)
	d := retryDecision{switchTo: switchTo, message: message}
	if d.switchTo != "http://127.0.0.1:7890" {
		t.Errorf("应切到全局代理，实际 %q", d.switchTo)
	}
	if !strings.Contains(d.message, "已自动切换代理") {
		t.Errorf("话术应说明已切换，实际 %q", d.message)
	}
}

// Test判定_任务钉死了代理就不动它 用户特意配的代理不该被系统悄悄改掉。
func Test判定_任务钉死了代理就不动它(t *testing.T) {
	switchTo, message := decideProxySwitch("", "http://user-proxy:1080", "http://127.0.0.1:7890", true)
	d := retryDecision{switchTo: switchTo, message: message}
	if d.switchTo != "" {
		t.Errorf("任务已钉死代理，不应被改写，实际切到了 %q", d.switchTo)
	}
	if strings.Contains(d.message, "已自动切换代理") {
		t.Errorf("没有真切换就不该这么说，实际 %q", d.message)
	}
}

// Test判定_没配全局代理时无从切换 无全局代理 + 直连失败 → 没有可切的出口。
func Test判定_没配全局代理时无从切换(t *testing.T) {
	switchTo, message := decideProxySwitch("", "", "", true)
	d := retryDecision{switchTo: switchTo, message: message}
	if d.switchTo != "" {
		t.Errorf("没有全局代理就不该切，实际 %q", d.switchTo)
	}
	if d.message != "" {
		t.Errorf("无切换可言时不应有话术，实际 %q", d.message)
	}
}

// Test判定_非网络错误不切代理 平台类错误（404/需登录）换出口也没用。
func Test判定_非网络错误不切代理(t *testing.T) {
	switchTo, message := decideProxySwitch("", "", "http://127.0.0.1:7890", false)
	d := retryDecision{switchTo: switchTo, message: message}
	if d.switchTo != "" {
		t.Errorf("非网络错误不该切代理，实际切到了 %q", d.switchTo)
	}
}

// Test出口决策_任务没配代理时全局代理本来就在用 出口决策的既有口径：
// 显式 > 任务级 > 全局 net.proxy > 直连。任务没配 proxy 时，
// **主出口就是全局代理**——这正是旧判定会误报「切换」的根因。
func Test出口决策_任务没配代理时全局代理本来就在用(t *testing.T) {
	primary, fallback := resolveAttempts("", "", "http://127.0.0.1:7890")
	if primary != "http://127.0.0.1:7890" {
		t.Errorf("任务没配代理时主出口应是全局代理，实际 %q", primary)
	}
	if fallback != "" {
		t.Errorf("主出口是代理时备用应为直连，实际 %q", fallback)
	}
}
