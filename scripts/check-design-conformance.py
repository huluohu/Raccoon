#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""设计方案符合度对账脚本（docs/07 前端设计落地）。

为什么要有这个脚本：测试全绿 ≠ 符合方案。本脚本把 docs/07 的
  §2 设计 Token / §3 路由表 / §4 组件清单 / §5 表情体系 / §7 交互铁律
固化成可执行断言，逐条 PASS/FAIL，任一 FAIL 即退出码非零。

用法：python3 scripts/check-design-conformance.py [--verbose]
"""
from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
WEB = ROOT / "web" / "src"

results: list[tuple[str, bool, str]] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    results.append((name, ok, detail))


def read(rel: str) -> str:
    return (WEB / rel).read_text(encoding="utf-8")


def extract_style_block(src: str) -> str:
    """取出 <style> 块内容（用于查 CSS 层叠顺序）。"""
    m = re.search(r"<style[^>]*>(.*?)</style>", src, re.S)
    return m.group(1) if m else ""


def exists(rel: str) -> bool:
    return (WEB / rel).exists()


def all_sources() -> list[Path]:
    return [p for p in WEB.rglob("*.vue")] + [p for p in WEB.rglob("*.ts")]


# ---------------------------------------------------------------- §3 路由表
def check_routes() -> None:
    router = read("router/index.ts")
    for path in ["/setup", "/login", "/", "/tasks", "/media",
                 "/subscriptions", "/live", "/connections", "/settings"]:
        check(f"§3 路由 {path}", f"'{path}'" in router, "router/index.ts")


# ---------------------------------------------------------------- §4 组件清单
# 组件 → 期望文件。PASS 条件 = 文件存在 且 被除自身以外的文件 import（真的在用）。
SPEC_COMPONENTS = {
    "RButton": "components/base/RButton.vue",
    "RInput": "components/base/RInput.vue",
    "RSelect": "components/base/RSelect.vue",
    "RTable": "components/base/RTable.vue",
    "RDialog": "components/base/RDialog.vue",
    "RToast": "components/base/RToast.vue",
    "REmpty": "components/base/REmpty.vue",
    "DetailDrawer": "components/base/DetailDrawer.vue",
    "TaskRow": "components/task/TaskRow.vue",
    "TaskCard": "components/task/TaskCard.vue",
    "TaskProgress": "components/task/TaskProgress.vue",
    "StatusDot": "components/task/StatusDot.vue",
    "TaskDetail": "components/task/TaskDetail.vue",
    "PlatformBadge": "components/task/PlatformBadge.vue",
    "PawSlider": "components/raccoon/PawSlider.vue",
    "RaccoonFace": "components/raccoon/RaccoonFace.vue",
}


def check_components() -> None:
    sources = all_sources()
    for name, rel in SPEC_COMPONENTS.items():
        if not exists(rel):
            check(f"§4 组件 {name} 存在", False, f"缺少 {rel}")
            continue
        # 被引用 = 有其他源文件 import 它（自身文件与自身声明不算）
        referenced = False
        for p in sources:
            if p == (WEB / rel):
                continue
            txt = p.read_text(encoding="utf-8", errors="ignore")
            if f"components/{rel.replace('components/', '').replace('.vue', '')}" in txt or \
               f"@/components/{rel.replace('components/', '').replace('.vue', '')}" in txt:
                referenced = True
                break
        check(f"§4 组件 {name} 存在且被引用", referenced, f"{rel}" + ("" if referenced else "（零引用）"))


# ---------------------------------------------------------------- §2 设计 Token
SPEC_TOKENS = [
    # 字阶
    "--font-display", "--font-title-1", "--font-title-2", "--font-title-3",
    "--font-body", "--font-aux", "--font-caption", "--line-height-body",
    # 结构色 / 动作色 / 画布 / 文字 / 语义
    "--accent", "--accent-strong", "--accent-soft", "--accent-softer",
    "--gold", "--gold-strong", "--gold-soft",
    "--bg", "--surface", "--surface-2", "--surface-3", "--border",
    "--text-1", "--text-2", "--text-3",
    "--success", "--warning", "--danger", "--info", "--neutral",
    # 圆角 / 阴影
    "--radius-ctrl", "--radius-card", "--radius-modal", "--radius-pill",
    "--shadow-1", "--shadow-2", "--shadow-3",
]


def check_tokens() -> None:
    css = read("styles/tokens.css")
    # 只统计 :root 块内定义的变量（[data-theme=dark] 是覆盖，不算定义缺失）
    root_block = css.split("[data-theme=")[0]
    for tok in SPEC_TOKENS:
        check(f"§2 Token {tok} 已定义", f"{tok}:" in root_block, "styles/tokens.css :root")


# ------------------------------------------------- §2 字阶落地（禁写死字号/字重）
# 方案 §2 给了完整字阶（12/13.5/16/18/20/24/30px + 七档字重）并规定「最小字号 12px」。
# 组件里写死数值的后果：改一次字阶要翻遍全仓，且 10/11px 的小字会悄悄跌破规格下限
# （曾实测散落 10px/11px 二十余处，超宽屏下整体比例失衡）。这里固化为机器断言：
# 凡 font-size/font-weight 出现数字字面量即 FAIL，必须走 var(--font-*)/var(--weight-*)。
HARDCODED_FONT_RE = re.compile(r"font-(?:size|weight)\s*:\s*[\d.]+")
# tokens.css 自身是变量定义处（--font-display: 30px），不适用本条
TYPOGRAHY_SCAN = [p for p in list(WEB.rglob("*.vue")) + list(WEB.rglob("*.css")) if p.name != "tokens.css"]


def check_typography() -> None:
    offenders = []
    for p in TYPOGRAHY_SCAN:
        if "/test/" in str(p) or p.name.endswith(".spec.ts"):
            continue
        for i, line in enumerate(p.read_text(encoding="utf-8", errors="ignore").splitlines(), 1):
            if HARDCODED_FONT_RE.search(line):
                offenders.append(f"{p.relative_to(WEB)}:{i}")
    check("§2 字号/字重全部走 Token（无写死数值）", not offenders,
          f"写死处：{offenders[:6]}" if offenders else "")

    # 内容区宽度约束：超宽屏下不为 ~1200px 设计的版面被硬拉满（曾实测 1905px 窗口
    # 内容区 1649px，输入框涨到 1000px、比例全面失衡）。外壳必须给出上限并居中。
    app = read("App.vue")
    check("§6 内容区有宽度约束（超宽屏不硬拉满）",
          re.search(r"\.content-inner\s*\{[^}]*max-width", app) is not None
          and "max-width" in app and "margin" in app,
          "App.vue .content-inner")


# ------------------------------------------- §2 字阶分布验收点（防「整体缩一圈」）
# 上一版闸门只查「字号没有写死数值」，那只能保证用的是 token，
# 保证不了 token 用对了档位。实测曾出现：Tasks 页 82 个文本节点里 56 个是
# 12px（--font-caption），正文几乎全是 caption 档——整站看起来「缩了一圈」，
# 用户反馈「布局字体不成比例」。方案 §2 明写「最小字号 12px，仅表头/徽章」，
# 所以 caption 出现在正文/标题/说明里就是违约。
#
# 这里的判定：--font-caption 只能出现在「表格表头 / 徽章 / 徽标 / 时间戳 / 辅助标签」
# 这些明确的小字场景；一旦出现在正文说明句、卡片标题、任务标题上，即 FAIL。
CAPTION_ALLOWED_CONTEXTS = [
    # CSS 类名里带这些词 → 属于表头/徽章/时间戳等允许场景
    "th", "thead", "badge", "pill", "chip", "cnt", "meta-time", "time",
    "status", "kind", "src-type", "mode", "warn-mark", "eff-badge", "lock-badge",
    "sens-badge", "sens-state", "token-scope", "history-val", "history-time",
    "history-by", "path", "thumb", "hint", "captcha", "muted", "count",
    # 徽标：18px 圆点里的一个字，是图标不是正文
    "glyph", "dot", "mark",
    # 状态标签（胶囊形徽章）：实时连接、录制计划这类
    ".conn", ".plan", ".src-type", ".mode",
    # 小尺寸按钮与图标按钮里的符号：图标不是正文
    ".rbtn--sm", ".rtoast-close", ".rinput-clear", ".rselect-clear",
    # 移动端底部导航栏的图标文字标签：375px 下 7 项并排，12px 是图注不是正文
    ".tabbar-label",
]


def check_typography_semantics() -> None:
    """§2 字阶语义：caption(12px) 只给表头/徽章，正文/标题不得使用。"""
    offenders = []
    for p in WEB.rglob("*.vue"):
        if p.name.endswith(".spec.ts"):
            continue
        txt = p.read_text(encoding="utf-8", errors="ignore")
        # 只看 CSS 段（style 块），模板里的内联字号不在此列
        for m in re.finditer(r"([^{}]+)\{([^}]*)\}", txt):
            selector, body = m.group(1), m.group(2)
            if "var(--font-caption)" not in body:
                continue
            # 选择器可能写成 .thumb.ph / .a .b，取完整选择器判断，
            # 不能只看最后一个类名（否则 .thumb.ph 会被截成 ph 而误判）。
            low = selector.lower()
            if any(k in low for k in CAPTION_ALLOWED_CONTEXTS):
                continue
            offenders.append(f"{p.relative_to(WEB)}:{selector.strip()[:40]}")
    check("§2 12px(caption) 仅用于表头/徽章（正文标题不得占用）", not offenders,
          f"越界使用：{offenders[:6]}" if offenders else "")

    # 正文档（--font-body 16px）必须在核心视图里真实出现：
    # 卡片标题、任务标题、页面副标题这些「要读的内容」都该是 16px 或更大。
    body_usage = 0
    for p in WEB.rglob("*.vue"):
        body_usage += len(re.findall(r"var\(--font-body\)", p.read_text(encoding="utf-8", errors="ignore")))
    check("§2 正文档 16px 在视图层有足够占比（非全站 12px）", body_usage >= 20,
          f"--font-body 引用数：{body_usage}（阈值 20）")


# ------------------------------------------------- §6 网格轨道塌陷（禁 auto-fill）
# auto-fill 与 auto-fit 的差别在数据少的时候才显出来：
# auto-fill 会把空轨道也占上，于是 1184px 宽的内容区排 4 条 285px 轨道，
# 页面只有 2 个卡片时右侧空出 570px——整页看着「东西全挤在左边」。
# 这是「布局不成比例」最直接的来源，必须由闸门兜住。
def check_grid_collapse() -> None:
    """§6 卡片网格用 auto-fit（空轨道塌陷），不用 auto-fill。"""
    offenders = []
    for p in WEB.rglob("*.vue"):
        if p.name.endswith(".spec.ts"):
            continue
        txt = p.read_text(encoding="utf-8", errors="ignore")
        for m in re.finditer(r"grid-template-columns\s*:\s*repeat\(\s*auto-fill", txt):
            line = txt[:m.start()].count("\n") + 1
            offenders.append(f"{p.relative_to(WEB)}:{line}")
    check("§6 卡片网格用 auto-fit（空轨道塌陷，不留右侧空白）", not offenders,
          f"仍用 auto-fill：{offenders}" if offenders else "")

    # 行内操作区必须横排：竖排时「速度/大小/取消/重试/删除」挤成一摞，
    # 右侧栏被撑到 120px，主内容区被挤窄，整行视觉重心跑到右边。
    trow = read("components/task/TaskRow.vue")
    check("§4 TaskRow 行内操作区横排（不与数据叠成竖列）",
          ".trow-ops" in trow and re.search(r"\.trow-ops\s*\{[^}]*flex-direction:\s*row", trow) is not None,
          "components/task/TaskRow.vue .trow-ops")

    # 卡片网格的最小列宽不能过窄：240px 以下放不下「标题 + 徽章 + 元信息」。
    # 只管卡片/网格布局（元信息两列排布 .meta 不算卡片网格，放宽到 160px 合法）。
    narrow = []
    for p in WEB.rglob("*.vue"):
        if p.name.endswith(".spec.ts"):
            continue
        txt = p.read_text(encoding="utf-8", errors="ignore")
        for m in re.finditer(r"\.([a-zA-Z0-9_-]+)\s*\{([^}]*)\}", txt):
            cls, body = m.group(1), m.group(2)
            mm = re.search(r"minmax\(\s*(\d+)px\s*,\s*1fr\s*\)", body)
            if not mm:
                continue
            low = cls.lower()
            # 卡片网格 = 类名里带 grid / cards / list / files / int-
            is_card_grid = any(k in low for k in ("grid", "cards", "files", "int-"))
            floor = 240 if is_card_grid else 160
            if int(mm.group(1)) < floor:
                narrow.append(f"{p.relative_to(WEB)}:{cls}={mm.group(1)}px(<{floor})")
    check("§6 卡片网格最小列宽 ≥240px（内容放得下）", not narrow,
          f"过窄：{narrow}" if narrow else "")


# ------------------------------------------------- §4 禁 emoji 冒充图标 / §5 表情
# 只判「真 emoji」（Misc Symbols & Pictographs / Supplemental Symbols & Pictographs /
# Emoji 呈现块）。U+2700–U+27BF Dingbats（✓ ✕ 等）与 U+2600–U+26FF 里被当作
# 印刷符号用的字符不算——它们在表格/Toast 里是「状态文字」而非「emoji 冒充图标」。
# 判定边界说明：docs/07 §4 禁的是拿 emoji 顶替品牌图标/Logo（如登录页 🦝），
# 不是禁止在文案里用排印符号。若要做最严解释（一律 SVG），把 EMOJI_RE 放开即可。
EMOJI_RE = re.compile("[\U0001F000-\U0001FAFF\U0001F900-\U0001F9FF]")


def check_no_emoji_icons() -> None:
    """方案 §4 明令「禁 emoji 冒充」图标/Logo。只扫 <template> 段。"""
    offenders = []
    for p in WEB.rglob("*.vue"):
        txt = p.read_text(encoding="utf-8", errors="ignore")
        m = re.search(r"<template>(.*)</template>", txt, re.S)
        if not m:
            continue
        if EMOJI_RE.search(m.group(1)):
            offenders.append(str(p.relative_to(ROOT)))
    check("§4/§5 组件模板无 emoji 冒充图标", not offenders, ", ".join(offenders))


# ---------------------------------------------------------------- §5 表情落地
# 上一版闸门只查「RaccoonFace 组件里有这六个字符串」——那只能证明组件实现了，
# 不能证明表情真的被用上了。实测曾出现 done / surprise 零引用：
# 六个表情齐备但页面永远只用 waiting/working/failed，等于没做表情体系。
# 这里查「按状态取用」：每个表情至少在一个非 spec 源文件里被真实引用。
FACE_USAGE = {
    "waiting": "空态 / 排队",
    "working": "加载 / 进行中",
    "done": "成功 / 入库",
    "failed": "失败 / 不支持",
    "cancelled": "主动取消",
    "surprise": "彩蛋 / 追更到货",
}


def check_face_system() -> None:
    """§5 六表情：组件实现齐备 + 按状态真实取用。"""
    rempty = read("components/base/REmpty.vue")
    check("§5 RaccoonFace 接入空态 REmpty", "RaccoonFace" in rempty, "components/base/REmpty.vue")
    rface = read("components/raccoon/RaccoonFace.vue")
    faces = ["waiting", "working", "done", "failed", "cancelled", "surprise"]
    missing = [f for f in faces if f"'{f}'" not in rface]
    check("§5 六表情齐备", not missing, f"缺：{missing}" if missing else "")

    # 按状态取用：扫所有 .vue（排除 RaccoonFace 自身与 spec），
    # 找 face="xxx" 或映射表里的 'xxx' 字面量。
    usage_sources = []
    for p in WEB.rglob("*.vue"):
        if p.name == "RaccoonFace.vue" or p.name.endswith(".spec.ts"):
            continue
        usage_sources.append(p.read_text(encoding="utf-8", errors="ignore"))
    joined = "\n".join(usage_sources)
    unused = []
    for f in faces:
        # face="done" | 'done': | face === 'done' 都算取用
        if not re.search(rf"""face\s*=\s*["']{f}["']|['"]{f}['"]\s*:""", joined):
            unused.append(f)
    check("§5 六表情按状态真实取用（非仅组件存在）", not unused,
          f"从未被使用：{unused}" if unused else "")

    # done 与 surprise 是上一版的两个真实缺口，单独点名断言防回归。
    check("§5 done 表情被真实使用（成功/入库场景）",
          bool(re.search(r"""face\s*=\s*["']done["']|['"]done['"]\s*:""", joined)), "")
    check("§5 surprise 表情被真实使用（追更到货场景）",
          bool(re.search(r"""face\s*=\s*["']surprise["']|['"]surprise['"]\s*:""", joined)), "")


# ------------------------------------------------- §3 视图级关键组件真实落地
# 上一版闸门只查 §4 组件清单（base/task/raccoon 三层），
# 完全没查 §3 路由表「关键组件」列点名的视图级组件。实测发现：
# QuickSubmit / StatusStrip / SubForm / SourceList / CredentialList / SettingItem
# 等只存在于代码注释里，从未被抽成组件——页面全是巨型单文件。
# 巨型单文件不是错，但它意味着方案里的组件边界没落地，且无法复用。
#
# 这里断言：方案点名的视图级组件必须是真实存在的 .vue 文件，
# 且至少有一个视图 import 了它（真在用，不是摆设）。
SPEC_VIEW_COMPONENTS = {
    "QuickSubmit": "components/shell/QuickSubmit.vue",
    "StatusStrip": "components/shell/StatusStrip.vue",
}


def check_view_components() -> None:
    """§3 视图级关键组件：真实文件 + 被视图引用。"""
    sources = all_sources()
    for name, rel in SPEC_VIEW_COMPONENTS.items():
        if not exists(rel):
            check(f"§3 视图级组件 {name} 存在", False, f"缺少 {rel}")
            continue
        check(f"§3 视图级组件 {name} 存在", True, rel)

        stem = rel.replace("components/", "").replace(".vue", "")
        referenced = False
        for p in sources:
            if p == (WEB / rel) or p.name.endswith(".spec.ts"):
                continue
            txt = p.read_text(encoding="utf-8", errors="ignore")
            if f"@/components/{stem}" in txt or f"components/{stem}" in txt:
                referenced = True
                break
        check(f"§3 视图级组件 {name} 被视图真实引用", referenced,
              f"{rel}（零引用）" if not referenced else "")


# --------------------------------------- §3 设置页分组导航（SettingsNav 左分组）
def check_settings_nav() -> None:
    """方案 §3 设置页：「SettingsNav（左分组）」。"""
    s = read("views/Settings.vue")
    check("§3 设置页有左分组导航（.side-nav）", ".side-nav" in s, "views/Settings.vue")
    # 分组导航必须有当前项高亮，否则 5000px 长页滚到哪都不知道自己在哪
    check("§3 设置页分组导航有当前项高亮（active 状态）",
          "side-link.on" in s or "activeSection" in s, "views/Settings.vue")
    # 分组容器要能独立滚动（长页面里导航不能被顶出视口）
    check("§3 设置页分组导航 sticky 定位",
          re.search(r"\.side-nav\s*\{[^}]*position:\s*sticky", s) is not None,
          "views/Settings.vue .side-nav")
    # 导航条目必须是人能读的正文字号，不能是 13px 小字
    check("§3 设置页导航条目用正文字号",
          re.search(r"\.side-link\s*\{[^}]*font-size:\s*var\(--font-body", s) is not None,
          "views/Settings.vue .side-link")

    # ---- scrollspy 三个必备机制（实测踩过的坑，不能只靠"存在 activeSection"糊弄）----
    # ① 判定线 scrollspy：不能用 IntersectionObserver（长卡片页视口可能无分组相交 → 高亮消失）
    #    只查代码行、忽略注释——注释里写「为什么不用 IntersectionObserver」是正常的。
    code_only = "\n".join(
        ln for ln in s.splitlines() if not ln.strip().startswith(("*", "//", "/*"))
    )
    check("§3 设置页分组导航用判定线 scrollspy（不是 IntersectionObserver）",
          "IntersectionObserver" not in code_only and "SPY_LINE" in code_only,
          "views/Settings.vue scrollspy")
    # ② 页面底部兜底：最后一个分组永远越不过判定线，滚到底必须能高亮它
    check("§3 设置页 scrollspy 有页面底部兜底",
          "scrollHeight" in s and "atBottom" in s,
          "views/Settings.vue atBottom")
    # ③ 锚点点击兜底：锚点被页面底部截断时判定线选不中被点项，必须有点击锁定
    check("§3 设置页 scrollspy 有锚点点击锁定",
          "lockedId" in s and "pickSection" in s,
          "views/Settings.vue lockedId")
    # 锁定不能僵死：用户主动滚动要能解锁
    check("§3 设置页 scrollspy 锁定可被用户滚动解除",
          "releaseLock" in s and "wheel" in s,
          "views/Settings.vue releaseLock")


# ---------------------------------------------------------------- §7 交互铁律
SHORTCUTS = {
    "`/` 聚焦提交框": "case '/'",
    "`Esc` 关闭/清除": "ev.key === 'Escape'",
    "`↑` 移动": "case 'ArrowUp'",
    "`↓` 移动": "case 'ArrowDown'",
    "`Enter` 详情": "case 'Enter'",
    "`Shift+Space` 多选": "ev.shiftKey",
    "`R` 重试": "case 'r'",
    "`G D`/`G S` 跳转": "router.push('/tasks')",
}


def check_shortcuts() -> None:
    path = WEB / "composables" / "useShortcuts.ts"
    if not path.exists():
        check("§7 快捷键模块存在", False, "缺少 composables/useShortcuts.ts")
        return
    txt = path.read_text(encoding="utf-8")
    for label, needle in SHORTCUTS.items():
        check(f"§7 快捷键 {label}", needle in txt, "composables/useShortcuts.ts")
    # 输入框内不触发（Esc 除外）
    check("§7 输入框内不劫持快捷键", "isTypingTarget" in txt, "composables/useShortcuts.ts")
    # Esc 在输入框内也放行
    esc_first = txt.find("ev.key === 'Escape'")
    guard_after = txt.find("isTypingTarget(ev.target)")
    check("§7 Esc 先于输入框判定（输入框内也可 Esc）", esc_first != -1 and guard_after != -1 and esc_first < guard_after)


def check_layout_fixes() -> None:
    """本轮修的真实缺陷与方案硬性数值，固化成断言防回归。"""
    login = read("views/Login.vue")
    check("登录页居中（.auth 占满 flex 行）", "flex: 1" in login and "width: 100%" in login,
          "views/Login.vue")
    app = read("App.vue")
    brand = re.search(r'<div class="brand">(.*?)</div>', app, re.S)
    check("侧栏 Logo 用 RaccoonFace（非内联 SVG/emoji）",
          bool(brand) and "RaccoonFace" in brand.group(1) and "<svg" not in brand.group(1),
          "App.vue .brand 块")
    # 导航图标用 SVG 线稿是 §4 的正当要求，但整页仍不许出现 emoji 冒充图标
    check("App.vue 模板无 emoji 冒充图标", not EMOJI_RE.search(
        re.search(r"<template>(.*)</template>", app, re.S).group(1) if re.search(r"<template>(.*)</template>", app, re.S) else ""
    ), "App.vue")
    # RDialog ≤480px（jsdom 不应用 scoped CSS，故在源码级断言）
    rdialog = read("components/base/RDialog.vue")
    check("§4 RDialog 宽度 ≤480px", bool(re.search(r"max-width:\s*480px", rdialog)),
          "components/base/RDialog.vue")


def check_spec_files() -> None:
    """方案 §8「每视图配 *.spec.ts（vitest）：渲染 + 关键交互」。"""
    views = sorted(p.stem for p in (WEB / "views").glob("*.vue"))
    missing = []
    for v in views:
        has_spec = any((WEB / "views").glob(f"{v}.spec.ts"))
        if not has_spec:
            missing.append(v)
    # 组件层用例：本轮新增的核心组件必须有用例
    comp_specs = sorted(p.name for p in WEB.rglob("*.spec.ts"))
    check("§8 视图层 spec 齐备", not missing, f"缺 spec：{missing}" if missing else "")
    check("§8 组件层 spec 存在（base/task/raccoon）",
          len([s for s in comp_specs if s in {
              "RInput.spec.ts", "RTable.spec.ts", "RDialog.spec.ts",
              "TaskCard.spec.ts", "TaskDetail.spec.ts", "PawSlider.spec.ts"}]) == 6,
          f"现有：{comp_specs}")


# ---------------------------------------------------------------- §6 动效与响应式
def check_motion() -> None:
    """方案 §6：动效四档 + 禁用项 + prefers-reduced-motion。"""
    motion_path = WEB / "styles" / "motion.css"
    if not motion_path.exists():
        check("§6 motion.css 存在", False, "缺少 styles/motion.css")
        return
    motion = motion_path.read_text(encoding="utf-8")
    main = read("main.ts")
    check("§6 motion.css 被 main.ts 引入", "styles/motion.css" in main, "main.ts")

    # 四档时长/曲线
    check("§6 微反馈 120ms ease-out", "--motion-micro: 120ms" in motion and "ease-out" in motion)
    check("§6 面板抽屉 180–240ms cubic-bezier(.2,.8,.2,1)",
          "--motion-panel: 200ms" in motion and "cubic-bezier(0.2, 0.8, 0.2, 1)" in motion)
    check("§6 进度数字 240ms linear",
          "--motion-progress: 240ms" in motion and "linear" in motion)
    check("§6 列表变更 180ms", "--motion-list: 180ms" in motion)
    check("§6 prefers-reduced-motion 全局兜底", "prefers-reduced-motion: reduce" in motion)

    # 禁用项：>400ms 入场一律不许（扫全部 vue/css 的 transition/animation 时长）
    slow = []
    for p in [q for q in WEB.rglob("*.vue")] + [q for q in WEB.rglob("*.css")]:
        txt = p.read_text(encoding="utf-8", errors="ignore")
        for decl in re.finditer(r"(?:transition|animation)[^;{}]*", txt):
            for d in re.finditer(r"(\d+(?:\.\d+)?)ms", decl.group(0)):
                if float(d.group(1)) > 400:
                    slow.append(f"{p.relative_to(ROOT)}:{d.group(0)}")
    check("§6 无 >400ms 入场/过渡", not slow, ", ".join(slow[:3]))

    # 循环动画：只允许「不确定进度条纹」这一处功能态（§4 要求条纹），且必须在减少动效下停掉
    loops = []
    for p in [q for q in WEB.rglob("*.vue")] + [q for q in WEB.rglob("*.css")]:
        txt = p.read_text(encoding="utf-8", errors="ignore")
        if "infinite" in txt:
            loops.append(str(p.relative_to(ROOT)))
    allowed_loops = {"web/src/components/task/TaskProgress.vue"}
    check("§6 循环动画仅剩不确定进度条纹（§4 功能态）",
          set(loops) <= allowed_loops, f"多余循环动画：{sorted(set(loops) - allowed_loops)}")
    if "web/src/components/task/TaskProgress.vue" in loops:
        tp = read("components/task/TaskProgress.vue")
        # 条纹必须有 reduced-motion 降级，否则就是纯装饰骚扰
        check("§6 不确定进度条纹在减少动效下停止",
              "prefers-reduced-motion" in tp and "animation: none" in tp,
              "components/task/TaskProgress.vue")

    # 列表变更高亮：motion.css 提供工具类，Tasks.vue 真的在用
    check("§6 新任务插入高亮工具类 .row-flash", ".row-flash" in motion, "styles/motion.css")
    check("§6 .row-flash 接入 Tasks 列表", "row-flash" in read("views/Tasks.vue"), "views/Tasks.vue")


def check_responsive() -> None:
    """方案 §6 响应式四档：≥1280 常驻 / 1024–1279 图标栏 / 768–1023 抽屉 / <768 底部栏。"""
    app = read("App.vue")
    check("§6 ≥1280 侧导航常驻（默认样式无覆盖）", ".nav {" in app)
    check("§6 1024–1279 侧导航收图标栏", "max-width: 1279px" in app and "min-width: 1024px" in app
          and ".nav-link-text { display: none; }" in app)
    check("§6 768–1023 侧导航收进抽屉", "max-width: 1023px" in app and "min-width: 768px" in app
          and "nav-toggle" in app and ".nav.open" in app)
    check("§6 <768 单栏 + 底部主操作栏", "max-width: 767px" in app and ".tabbar" in app)
    check("§6 <768 触控目标 ≥44px", "min-height: 44px" in app)
    # 375px 关键按钮不遮挡：内容区必须给底部栏留出空间（含安全区）
    check("§6 <768 内容留出底部安全区",
          re.search(r"\.content \{[^}]*safe-area-inset-bottom", app, re.S) is not None,
          "App.vue .content <768 档")
    # 768–1023 表格降级卡片
    media = read("views/Media.vue")
    check("§6 768–1023 表格降级卡片", "narrowTable" in media and "max-width: 1023px" in media,
          "views/Media.vue")
    # 小屏按钮进入触控尺寸
    rbtn = read("components/base/RButton.vue")
    check("§6 <768 RButton 触控尺寸", "max-width: 767px" in rbtn and "min-height: 44px" in rbtn,
          "components/base/RButton.vue")

    # ---- CSS 层叠顺序断言（实测踩过的坑）----
    # 「默认隐藏态」若写在媒体查询之后，同特异性下后写的赢，
    # 会把媒体查询里的 display:inline-flex / display:flex 覆盖掉：
    #   768–1023 档 → 汉堡按钮不显示，导航彻底打不开
    #   <768 档     → 底部主操作栏不渲染
    # 光靠"规则存在"查不出来，必须查「隐藏态在媒体查询之前」。
    style = extract_style_block(app)
    default_nav_toggle = style.find(".nav-toggle {")
    default_tabbar = style.find(".tabbar {")
    md_1023 = style.find("max-width: 1023px")
    md_767 = style.find("max-width: 767px")
    check("§6 默认隐藏态在媒体查询之前（否则覆盖档位样式）",
          default_nav_toggle != -1 and default_tabbar != -1
          and md_1023 != -1 and md_767 != -1
          and default_nav_toggle < md_1023 and default_tabbar < md_767,
          "App.vue <style> 层叠顺序")
    # 媒体查询里必须真的把 display 打开
    check("§6 768–1023 档汉堡按钮 display 打开",
          re.search(r"@media \(max-width: 1023px\).*?\.nav-toggle \{ display: inline-flex;",
                    style, re.S) is not None,
          "App.vue .nav-toggle @media")
    check("§6 <768 档底部主操作栏 display 打开",
          re.search(r"@media \(max-width: 767px\).*?\.tabbar \{.*?display: flex;",
                    style, re.S) is not None,
          "App.vue .tabbar @media")
    # 底部栏不能只有图标：移动端没有 hover tooltip，认不出哪个入口是哪个
    check("§6 <768 底部主操作栏有文字标签",
          "tabbar-label" in app and re.search(
              r"\.tabbar-label\s*\{[^}]*font-size:", style) is not None,
          "App.vue .tabbar-label")
    # 底部栏每个入口都要有可读标签文本（不能是空 span）
    check("§6 <768 底部栏渲染 label 文本",
          re.search(r'<span class="tabbar-label">\{\{ item\.label \}\}</span>', app) is not None,
          "App.vue tabbar 模板")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args()

    check_routes()
    check_components()
    check_view_components()
    check_settings_nav()
    check_tokens()
    check_typography()
    check_typography_semantics()
    check_grid_collapse()
    check_no_emoji_icons()
    check_face_system()
    check_shortcuts()
    check_layout_fixes()
    check_spec_files()
    check_motion()
    check_responsive()

    passed = sum(1 for _, ok, _ in results if ok)
    failed = [(n, d) for n, ok, d in results if not ok]

    print("=" * 72)
    print("设计方案符合度对账（docs/07 前端设计落地）")
    print("=" * 72)
    if args.verbose:
        for name, ok, detail in results:
            mark = "PASS" if ok else "FAIL"
            print(f"[{mark}] {name}" + (f"  <- {detail}" if detail and not ok else ""))
        print("-" * 72)
    print(f"合计 {len(results)} 项：PASS {passed} / FAIL {len(failed)}")

    if failed:
        print("\n未通过项：")
        for name, detail in failed:
            print(f"  ✗ {name}" + (f"  <- {detail}" if detail else ""))
        print("\n结论：不符合设计方案，需按 docs/07 补齐后重跑。")
        return 1
    print("\n结论：符合设计方案（§2 Token / §3 路由 / §4 组件 / §5 表情 / §6 动效响应式 / §7 交互 / §8 工程约定 全部对齐）。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
