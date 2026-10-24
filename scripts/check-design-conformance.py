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
    # 复刻波（docs/15 T05）新增：图表与指标卡必须是真被视图用的组件，
    # 不是「文件建好了但没人引用」的摆设。
    "MetricCard": "components/base/MetricCard.vue",
    "TrendChart": "components/charts/TrendChart.vue",
    "DonutChart": "components/charts/DonutChart.vue",
    "DeleteFileDialog": "components/media/DeleteFileDialog.vue",
    # 控制台方向（docs/07 §2 页头签名）：十视图统一的指挥中心式页头。
    "PageHeader": "components/base/PageHeader.vue",
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
    # 等宽（眉题/时间戳/ID/日志/路径）
    "--font-mono",
    # 结构色 / 动作色 / 画布 / 文字 / 语义
    "--accent", "--accent-strong", "--accent-soft", "--accent-softer",
    "--gold", "--gold-strong", "--gold-soft",
    "--bg", "--surface", "--surface-2", "--surface-3", "--border",
    "--text-1", "--text-2", "--text-3",
    "--success", "--warning", "--danger", "--info", "--neutral",
    # 圆角 / 阴影
    "--radius-ctrl", "--radius-card", "--radius-modal", "--radius-pill",
    "--shadow-1", "--shadow-2", "--shadow-3",
    # 浣熊形象（四主题各自覆盖，换皮契约：docs/07 §2）
    "--logo-fur", "--logo-face", "--logo-mask", "--logo-scarf", "--logo-ear",
]

# 四主题的 id（docs/07 §2）：:root 即默认主题 night，其余三主题用 data-theme 覆盖。
THEME_IDS = ["scarf", "snow", "berry"]
# 每个主题块必须自带的核心 token（画布/文字/强调/导航底/CTA/浣熊形象）。
THEME_CORE_TOKENS = [
    "--bg", "--surface", "--surface-2", "--surface-3", "--border",
    "--text-1", "--text-2", "--text-3",
    "--accent", "--accent-strong", "--accent-soft",
    "--nav-bg", "--cta-bg", "--cta-fg",
    "--logo-fur", "--logo-face", "--logo-mask", "--logo-scarf", "--logo-ear",
    "--scrim", "--scrim-deep",
]


def check_tokens() -> None:
    css = read("styles/tokens.css")
    # 只统计 :root 块内定义的变量（[data-theme=…] 是覆盖，不算定义缺失）
    root_block = css.split("[data-theme=")[0]
    for tok in SPEC_TOKENS:
        check(f"§2 Token {tok} 已定义", f"{tok}:" in root_block, "styles/tokens.css :root")

    # 实色块上的文字色：必须走 --on-* 系列。
    # 为什么单列一条：强调色亮度随主题变，写死 #fff 迟早在某个主题上读不清。
    for tok in ("--on-accent", "--on-success", "--on-danger", "--on-info", "--on-neutral", "--on-warning"):
        check(f"§2 Token {tok} 已定义（实色块上的文字色）", f"{tok}:" in root_block,
              "styles/tokens.css :root")

    # 四套浣熊主题（docs/07 §2）：:root = night 默认；scarf/snow/berry 各一个覆盖块，
    # 每块必须自带全套核心 token——主题不是「改一个强调色」，是完整换皮（含浣熊形象）。
    blocks = re.findall(r"\[data-theme='([a-z]+)'\]", css)
    check("§2 四套浣熊主题齐全（night 默认 + scarf/snow/berry 覆盖块）",
          set(blocks) == set(THEME_IDS), f"发现主题块：{sorted(set(blocks))}")
    for theme in THEME_IDS:
        m = re.search(rf"\[data-theme='{theme}'\]\s*\{{(.*?)\n\}}", css, re.S)
        body = m.group(1) if m else ""
        missing = [t for t in THEME_CORE_TOKENS if t + ":" not in body]
        check(f"§2 主题 {theme} 定义全套核心 token（完整换皮）", not missing,
              f"缺：{missing}" if missing else "")
    # 主题形象必须真的不同：四套 --logo-scarf 值两两不同（换皮不是改名）。
    scarves = re.findall(r"--logo-scarf:\s*([^;]+);", css)
    check("§2 浣熊围巾色随主题变化（换皮而非换名）", len(set(scarves)) >= 4,
          f"发现 {len(set(scarves))} 种围巾色：{scarves}")


# ------------------------------------------- §2 禁硬编码色值（全形式扫描）
# 上一版只查 `#hex` 三件套，漏掉了：
#   - `#fff`（短写）
#   - `rgba(27, 33, 51, 0.32)`（遮罩层）
#   - `white` / `black`（具名色）
# 实测全站有 13 处 `color: #fff` 分布在 8 个文件——深色模式下全部对比度不合格。
# 这里把扫描扩到所有形式，并把「平台品牌色」单独豁免（外部客观事实，不是设计选择，
# 但品牌色块上的文字色仍需配对，见 PLATFORM_TONE_FG）。
COLOR_LITERAL_RE = re.compile(
    r"#[0-9a-fA-F]{3,8}\b"
    r"|rgba?\([^)]*\)"
    r"|hsla?\([^)]*\)"
    r"|(?<=[:\s])(white|black)(?=[\s;}])"
)
# 平台品牌色是外部事实（YouTube 红、微博红…），允许出现在 utils/format.ts。
COLOR_EXEMPT_FILES = {"styles/tokens.css"}
# 注释里的色值说明不算违规（要解释「浅色下 text-1 = #1B2133」这种来由）
COLOR_COMMENT_RE = re.compile(r"^\s*(/\*|\*|//)")


def check_no_hardcoded_colors() -> None:
    offenders = []
    for p in list(WEB.rglob("*.vue")) + list(WEB.rglob("*.css")):
        rel = str(p.relative_to(WEB))
        if rel in COLOR_EXEMPT_FILES or p.name.endswith(".spec.ts"):
            continue
        in_block_comment = False
        for i, line in enumerate(p.read_text(encoding="utf-8", errors="ignore").splitlines(), 1):
            # 注释要连带跳过：/* 开头后到 */ 结束的所有行都是注释
            # （实测踩过：RDialog 里解释「浅色下 text-1 = #1B2133」的续行注释被误判）
            stripped = line.strip()
            if in_block_comment:
                if "*/" in stripped:
                    in_block_comment = False
                continue
            if stripped.startswith("/*"):
                if "*/" not in stripped:
                    in_block_comment = True
                continue
            if COLOR_COMMENT_RE.match(line):
                continue
            # 平台品牌色调色板（外部客观事实）
            if "utils/format.ts" in rel and "PLATFORM_TONE" in line:
                continue
            if COLOR_LITERAL_RE.search(line):
                offenders.append(f"{rel}:{i}")
    check("§2 无硬编码色值（#hex/rgb()/hsl()/具名色全形式）", not offenders,
          f"写死处：{offenders[:8]}" if offenders else "")


# ------------------------------------------- §8 文件完整性（非法字符会静默破坏 SFC）
# 实测踩过：新建组件时注释里混进一个损坏的 UTF-8 字节（U+FFFD 替换字符），
# vue-tsc 报出来的却是一堆「PLATFORM_LABEL 不存在」「onSubmit 不存在」——
# 因为 SFC 的 script 块根本没被正确解析，模板里的标识符全成了「未定义」。
# 排查这类怪错误要先看文件本身有没有坏字符。
def check_source_integrity() -> None:
    offenders = []
    for p in list(WEB.rglob("*.vue")) + list(WEB.rglob("*.ts")):
        raw = p.read_bytes()
        rel = str(p.relative_to(WEB))
        try:
            text = raw.decode("utf-8")
        except UnicodeDecodeError:
            offenders.append(f"{rel}(非法字节)")
            continue
        if "�" in text:
            for i, line in enumerate(text.split("\n"), 1):
                if "�" in line:
                    offenders.append(f"{rel}:{i}")
    check("§8 源码无损坏字符（U+FFFD / 非法 UTF-8）", not offenders,
          f"坏字符处：{offenders[:6]}" if offenders else "")


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
    # 眉题（.eyebrow）：等宽大写小标签，是页头签名的一部分（docs/07 §2/§8.2）
    "eyebrow", "ph-sub", "nav-en", "nav-sec-zh", "clock",
    # 徽标：18px 圆点里的一个字，是图标不是正文；置顶胶囊是徽章式操作钮
    "glyph", "dot", "mark", "pin",
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
#
# 覆盖 §3 路由表「关键组件」列点名的全部组件（22 个）：
#   /setup        向导表单
#   /             QuickSubmit / ActiveTasks / IssuesPanel / StatusStrip
#   /tasks        TaskList / TaskDetail
#   /media        FileCard / PreviewDrawer / DownloadBtn
#   /subscriptions SubList / SubForm / ItemsTable
#   /live         SourceList / RecordPlanForm / RecordingList
#   /connections  CredentialList / QbTestPanel / TelegramPanel
#   /settings     SettingsNav / SettingItem
# （§3 的 /media 写「FileTable/FileCard」二选一，落地为 FileCard；
#   §3 的 /setup「向导表单」落地为 SetupWizard。）
SPEC_VIEW_COMPONENTS = {
    "SetupWizard": "components/auth/SetupWizard.vue",
    "QuickSubmit": "components/shell/QuickSubmit.vue",
    "ActiveTasks": "components/overview/ActiveTasks.vue",
    "IssuesPanel": "components/overview/IssuesPanel.vue",
    "StatusStrip": "components/shell/StatusStrip.vue",
    "TaskList": "components/task/TaskList.vue",
    "TaskDetail": "components/task/TaskDetail.vue",
    "FileCard": "components/media/FileCard.vue",
    "PreviewDrawer": "components/media/PreviewDrawer.vue",
    "DownloadBtn": "components/media/DownloadBtn.vue",
    "SubList": "components/subs/SubList.vue",
    "SubForm": "components/subs/SubForm.vue",
    "ItemsTable": "components/subs/ItemsTable.vue",
    "SourceList": "components/live/SourceList.vue",
    "RecordPlanForm": "components/live/RecordPlanForm.vue",
    "RecordingList": "components/live/RecordingList.vue",
    "CredentialList": "components/conn/CredentialList.vue",
    "QbTestPanel": "components/conn/QbTestPanel.vue",
    "TelegramPanel": "components/conn/TelegramPanel.vue",
    "SettingsNav": "components/settings/SettingsNav.vue",
    "SettingItem": "components/settings/SettingItem.vue",
}

# §2 页头签名（docs/07）：十个一级视图统一接入 PageHeader（眉题 + 句点大标题 + 副行 + actions）。
# Login/Setup 是独立品牌面板，不在其列。
VIEW_USAGE_PAGEHEADER = [
    "Overview", "Tasks", "History", "Media", "Subscriptions",
    "Live", "Logs", "Monitor", "Connections", "Settings",
]


def check_view_components() -> None:
    """§3 视图级关键组件：真实文件 + 被视图真实引用。"""
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


# ------------------------------------- §3 视图级组件：方案点名的视图必须真的用上
# 上一条只查「有人 import 了它」——但 import 了不等于用对了位置。
# 实测翻车：设置页把「生效时机徽章」写在视图里，组件抽出来了却没接上，
# 于是组件成了摆设（闸门全绿，页面照旧）。
# 这里加一条：方案点名的每个视图，必须真的在模板里用了对应的组件标签。
# 断言查的是「模板里出现该组件名」，不是「文件里出现过这个词」——
# 注释里写「本该用 TaskList」不算。
def check_view_component_usage() -> None:
    """§3 每个视图真的用了方案点名的组件（在模板里，不是注释里）。"""
    VIEW_USAGE = {
        "views/Overview.vue": ["ActiveTasks", "IssuesPanel", "StatusStrip"],
        "views/Tasks.vue": ["TaskList", "TaskDetail"],
        "views/Media.vue": ["FileCard", "PreviewDrawer", "DownloadBtn"],
        "views/Subscriptions.vue": ["SubList", "SubForm", "ItemsTable"],
        "views/Live.vue": ["SourceList", "RecordPlanForm", "RecordingList"],
        "views/Connections.vue": ["CredentialList", "QbTestPanel", "TelegramPanel"],
        "views/Settings.vue": ["SettingsNav", "SettingItem"],
        "views/Setup.vue": ["SetupWizard"],
    }
    for rel, names in VIEW_USAGE.items():
        path = WEB / rel
        if not path.exists():
            check(f"§3 视图 {rel} 存在", False, f"缺少 {rel}")
            continue
        txt = path.read_text(encoding="utf-8", errors="ignore")
        m = re.search(r"<template>(.*)</template>", txt, re.S)
        tpl = m.group(1) if m else ""
        missing = [n for n in names if not re.search(rf"<{n}[\s/>]", tpl)]
        check(f"§3 视图 {Path(rel).stem} 真的用了 {', '.join(names)}",
              not missing, f"模板里没用：{missing}" if missing else "")

    # QuickSubmit 是全局顶栏组件（方案 §3 全局条款「顶部 QuickSubmit」），
    # 物理上挂在外壳 App.vue，不在总览视图里——单独断言它挂在顶栏且能聚焦。
    app = read("App.vue")
    app_tpl_m = re.search(r"<template>(.*)</template>", app, re.S)
    app_tpl = app_tpl_m.group(1) if app_tpl_m else ""
    check("§3 全局 QuickSubmit 挂在顶栏外壳（App.vue）",
          "<QuickSubmit" in app_tpl, "App.vue")
    check("§3 QuickSubmit 有 id 供快捷键聚焦",
          re.search(r'id="[^"]*"', read("components/shell/QuickSubmit.vue")) is not None
          or "quick" in read("components/shell/QuickSubmit.vue").lower(),
          "components/shell/QuickSubmit.vue")


# --------------------------------------- §3 设置页分组导航（SettingsNav 左分组）
def check_settings_nav() -> None:
    """方案 §3 设置页：「SettingsNav（左分组）」。

    注意：scrollspy 的判定逻辑已随组件抽进 components/settings/SettingsNav.vue，
    这里必须查组件文件（查视图文件会误报「没有判定线」）。
    """
    s = read("views/Settings.vue")
    nav = read("components/settings/SettingsNav.vue")

    # 视图必须真的用上导航组件
    check("§3 设置页有左分组导航（SettingsNav 组件）",
          "<SettingsNav" in s, "views/Settings.vue")

    # 分组导航必须有当前项高亮，否则 5000px 长页滚到哪都不知道自己在哪
    check("§3 设置页分组导航有当前项高亮（active 状态）",
          "side-link.on" in nav or "activeSection" in nav,
          "components/settings/SettingsNav.vue")
    # 分组容器要能独立滚动（长页面里导航不能被顶出视口）
    check("§3 设置页分组导航 sticky 定位",
          re.search(r"\.side-nav\s*\{[^}]*position:\s*sticky", nav) is not None,
          "components/settings/SettingsNav.vue .side-nav")
    # 导航条目必须是人能读的正文字号，不能是 13px 小字
    check("§3 设置页导航条目用正文字号",
          re.search(r"\.side-link\s*\{[^}]*font-size:\s*var\(--font-body", nav) is not None,
          "components/settings/SettingsNav.vue .side-link")

    # ---- scrollspy 三个必备机制（实测踩过的坑，不能只靠"存在 activeSection"糊弄）----
    # ① 判定线 scrollspy：不能用 IntersectionObserver（长卡片页视口可能无分组相交 → 高亮消失）
    #    只查代码行、忽略注释——注释里写「为什么不用 IntersectionObserver」是正常的。
    code_only = "\n".join(
        ln for ln in nav.splitlines() if not ln.strip().startswith(("*", "//", "/*"))
    )
    check("§3 设置页分组导航用判定线 scrollspy（不是 IntersectionObserver）",
          "IntersectionObserver" not in code_only and "SPY_LINE" in code_only,
          "components/settings/SettingsNav.vue scrollspy")
    # ② 页面底部兜底：最后一个分组永远越不过判定线，滚到底必须能高亮它
    check("§3 设置页 scrollspy 有页面底部兜底",
          "scrollHeight" in nav and "atBottom" in nav,
          "components/settings/SettingsNav.vue atBottom")
    # ③ 锚点点击兜底：锚点被页面底部截断时判定线选不中被点项，必须有点击锁定
    check("§3 设置页 scrollspy 有锚点点击锁定",
          "lockedId" in nav and "pickSection" in nav,
          "components/settings/SettingsNav.vue lockedId")
    # 锁定不能僵死：用户主动滚动要能解锁
    check("§3 设置页 scrollspy 锁定可被用户滚动解除",
          "releaseLock" in nav and "wheel" in nav,
          "components/settings/SettingsNav.vue releaseLock")
    # 首帧坑：内容没撑开时 scrollHeight=0，不能误高亮到最后一个分组
    check("§3 设置页 scrollspy 有首帧兜底（scrollHeight=0 不误判到底）",
          re.search(r"docH\s*>\s*window\.innerHeight", nav) is not None,
          "components/settings/SettingsNav.vue docH 守卫")


# --------------------------------- §3 设置页 SettingItem：生效时机徽章 + 脏圆点 + 保存反馈
def check_setting_item() -> None:
    """方案 §3 设置页：「SettingItem（生效时机徽章 + 脏圆点 + 保存反馈）」。"""
    item = read("components/settings/SettingItem.vue")
    # ① 生效时机徽章：文案来自后端 effective，不是前端写死三选一
    check("§3 SettingItem 有生效时机徽章",
          "eff-badge" in item and "item.effective" in item,
          "components/settings/SettingItem.vue")
    # ② 脏圆点
    check("§3 SettingItem 有脏圆点", "dirty-pip" in item and "dirty" in item,
          "components/settings/SettingItem.vue")
    # ③ 保存反馈：逐项告知这一项什么时候生效（不能只有全局 toast）
    check("§3 SettingItem 有逐项保存反馈", "saved-badge" in item and "savedText" in item,
          "components/settings/SettingItem.vue")
    # 铁律：敏感项不回填掩码（回填后保存会把掩码写进配置）
    check("§3 SettingItem 敏感项不回填掩码",
          "sensitive" in item and "留空表示不修改" in item,
          "components/settings/SettingItem.vue")
    # env 锁定项只读
    check("§3 SettingItem env 锁定项只读",
          "item.locked" in item and "disabled" in item,
          "components/settings/SettingItem.vue")


# --------------------------------- §3 全局条款：诊断入口在设置页
def check_diag_entry() -> None:
    """方案 §3 全局条款：「诊断入口在设置页」。"""
    s = read("views/Settings.vue")
    panel = read("components/settings/DiagPanel.vue")
    check("§3 设置页有诊断分组", 'id="diag"' in s, "views/Settings.vue #diag")
    check("§3 设置页真的接了诊断面板", "<DiagPanel" in s, "views/Settings.vue")
    check("§3 诊断面板提供预览与导出两个入口",
          "diagPreview" in panel and "diagExport" in panel,
          "components/settings/DiagPanel.vue")


# --------------------------------- §3 /setup 初始化向导表单（独立于登录页）
def check_setup_wizard() -> None:
    """方案 §3「/setup | 初始化管理员 | 向导表单」。"""
    check("§3 初始化向导组件存在", exists("components/auth/SetupWizard.vue"),
          "components/auth/SetupWizard.vue")
    check("§3 /setup 路由指向独立视图",
          "views/Setup.vue" in read("router/index.ts"), "router/index.ts")
    # 向导必须分步（只有一个表单就是登录页兼职，不是向导）
    wizard = read("components/auth/SetupWizard.vue")
    check("§3 初始化向导是分步的", "第 {{ step }}" in wizard or "step" in wizard and "确认创建" in wizard,
          "components/auth/SetupWizard.vue")
    # 初始化向导要说明以后去哪改账号
    check("§3 初始化向导说明后续修改入口",
          "设置" in wizard and "账号" in wizard,
          "components/auth/SetupWizard.vue")


def check_engine_detail_visible() -> None:
    """任务详情要给出引擎真实原因（不能只有一句「引擎退出码 1」）。

    背景：error_message 是分类后的中文文案（"所选清晰度不可用"），
    说的是「属于哪一类问题」；引擎原文（yt-dlp 的 ERROR 行）说的是
    「到底报了什么」。只给前者，用户换清晰度/换链接/升级引擎全靠猜。
    后端已把原文写进 event='engine_error' 的事件里，前端必须把它提到错误区。
    """
    detail = read("components/task/TaskDetail.vue")
    check("§4 任务详情展示引擎原文（engine_error 事件）",
          "engine_error" in detail and "engineDetail" in detail,
          "components/task/TaskDetail.vue")
    check("§4 引擎原文有独立视觉标记（等宽 + 弱化底）",
          ".td-engine-detail" in detail and "mono" in detail,
          "components/task/TaskDetail.vue .td-engine-detail")
    # 后端确实把原文写进事件了（这条查后端，不是查前端）
    worker = (ROOT / "backend" / "internal" / "task" / "worker.go")
    wtxt = worker.read_text(encoding="utf-8", errors="ignore") if worker.exists() else ""
    check("§4 后端把引擎 stderr 摘要写进 engine_error 事件",
          'ee.Detail' in wtxt and '"engine_error"' in wtxt,
          "backend/internal/task/worker.go")


def check_visual_direction() -> None:
    """视觉方向层的验收点（「浣熊夜勤」控制台方向，docs/07 §2 四主题裁决后重写）。

    背景：零件级 + 构图级断言全绿之后，界面仍然可以「看起来业余、小家子气」。
    这一层查的是设计方向的三根支柱有没有真的立起来：

    1. 品牌：有静态徽标（RaccoonLogo），且配色走 --logo-* 换皮变量（四主题 = 四只浣熊）；
    2. 结构色：深底侧导航撑控制台骨架，token 按主题完整定义（不是只换个强调色）；
    3. 动作色：主 CTA 走 --cta-*（值随主题强调色），金色降级为品牌点缀；
    4. 页头签名：指挥中心式页头（眉题 + 句点大标题）归属视图层 PageHeader，顶栏不再放标题。

    断言全部落在可观测的代码形态上（变量引用 / 模板用法），不查注释。
    """
    # ① 品牌徽标组件存在且有独立用例
    check("§5 品牌徽标组件存在（RaccoonLogo，静态识别符号）",
          exists("components/brand/RaccoonLogo.vue"), "components/brand/RaccoonLogo.vue")
    check("§5 品牌徽标有独立用例",
          exists("components/brand/RaccoonLogo.spec.ts"), "components/brand/RaccoonLogo.spec.ts")

    logo = read("components/brand/RaccoonLogo.vue")
    # ② 识别三件套：连体眼罩 / 环纹尾 / 主题化围巾与内耳
    # 旧版眼罩是两条细斜带——缩到小尺寸就是哈士奇纹路；连体眼罩 + 环尾才钉死「浣熊」。
    check("§5 徽标含连体眼罩（单一实色块，禁两条斜带）",
          "M 10.5 28.5" in logo and "M11 27" not in logo and "M53 27" not in logo,
          "components/brand/RaccoonLogo.vue")
    check("§5 徽标含环纹尾（浣熊第二识别物）",
          "stroke-dasharray" in logo and 'stroke="var(--logo-fur)"' in logo,
          "components/brand/RaccoonLogo.vue")
    # 围巾走 --logo-scarf（随主题换皮），不再直接绑 --gold：
    # 四主题下浣熊围巾分别是薄荷/金/墨青/珊瑚，绑死金色就换不了皮。
    check("§5 徽标围巾走 --logo-scarf（主题换皮）",
          "var(--logo-scarf)" in logo and "var(--logo-scarf-strong)" in logo,
          "components/brand/RaccoonLogo.vue")
    check("§5 徽标内耳走 --logo-ear（主题换皮）",
          "var(--logo-ear)" in logo,
          "components/brand/RaccoonLogo.vue")
    # ③ 徽标配色走 --logo-* 变量：组件本身不猜环境，颜色随主题换皮。
    check("§5 徽标配色走 --logo-* 变量",
          "var(--logo-fur)" in logo and "var(--logo-face)" in logo and "var(--logo-mask)" in logo,
          "components/brand/RaccoonLogo.vue")
    # 六表情同走换皮变量：RaccoonFace 不得直接用 --accent/--surface 当毛色/脸底，
    # 否则四主题下会出现一只「页面色」的浣熊而不是换皮的浣熊。
    face = read("components/raccoon/RaccoonFace.vue")
    # 剥掉注释行再断言（§8.1 ②：断言查代码，注释里的设计说明不算违规）
    face_code = "\n".join(
        ln for ln in face.splitlines() if not ln.strip().startswith("//")
    )
    check("§5 六表情配色走 --logo-* 变量（禁 --accent/--surface 当毛色）",
          "var(--logo-fur)" in face_code and "var(--logo-face)" in face_code
          and "--accent" not in face_code and "--surface" not in face_code,
          "components/raccoon/RaccoonFace.vue")

    # ④ 品牌位都用徽标：侧导航 / 登录页 / 初始化向导
    app = read("App.vue")
    check("§5 侧导航品牌区用 RaccoonLogo（不拿状态表情当 Logo）",
          "RaccoonLogo" in app and "brand" in app,
          "App.vue")
    login = read("views/Login.vue")
    check("§5 登录页品牌区用 RaccoonLogo",
          "RaccoonLogo" in login, "views/Login.vue")
    wizard = read("components/auth/SetupWizard.vue")
    check("§5 初始化向导品牌区用 RaccoonLogo",
          "RaccoonLogo" in wizard, "components/auth/SetupWizard.vue")

    # ⑤ 侧导航深底：结构色大面积载体；token 在 :root 与每个主题块都要有（check_tokens 已查每主题块）。
    nav_css = app[app.find(".nav {"):app.find(".nav {") + 260]
    check("§2 侧导航用结构色深底（控制台骨架）",
          "var(--nav-bg)" in nav_css, "App.vue .nav")

    # ⑥ 主 CTA：全站最高频动作（顶栏提交按钮），用 --cta-* token（值随主题走强调色）。
    qs = read("components/shell/QuickSubmit.vue")
    btn = qs[qs.find(".qsubmit-btn {"):qs.find(".qsubmit-btn {") + 320]
    check("§2 顶栏主按钮用 CTA token",
          "var(--cta-bg)" in btn and "var(--cta-fg)" in btn,
          "components/shell/QuickSubmit.vue .qsubmit-btn")

    # ⑦ 主题切换器：四主题在 ui store 注册（含旧值迁移），侧栏可切换。
    ui = read("stores/ui.ts")
    for t in ["night", "scarf", "snow", "berry"]:
        check(f"§2 主题 {t} 在切换器注册", t in ui, "stores/ui.ts")

    # ⑧ 指挥中心式页头（docs/07 §2 页头签名）：PageHeader 承载眉题 + 大标题 + 副行，
    #    十视图统一接入；顶栏不再放页面标题（标题是页面构图的一部分）。
    check("§2 PageHeader 组件存在",
          exists("components/base/PageHeader.vue"), "components/base/PageHeader.vue")
    ph = read("components/base/PageHeader.vue")
    check("§2 PageHeader 含眉题结构（eyebrow）", "eyebrow" in ph,
          "components/base/PageHeader.vue")
    check("§2 PageHeader 含句点大标题（--font-display）",
          "var(--font-display)" in ph, "components/base/PageHeader.vue")
    missing_ph = [v for v in VIEW_USAGE_PAGEHEADER if f"<PageHeader" not in read(f"views/{v}.vue")]
    check("§2 十视图统一接入 PageHeader", not missing_ph,
          f"未接入：{missing_ph}" if missing_ph else "")
    check("§8 顶栏不再放页面标题（页头归属视图）", "topbar-title" not in app,
          "App.vue 不应再有 topbar-title")


def check_link_validation() -> None:
    """提交前的链接格式闸：分享文案能提取，缺协议头有提示。

    背景：B 站/抖音分享出来的是一整段带文案的文本，
    直接粘进后端会被协议白名单拒，用户看到的是看不懂的技术报错。
    """
    check("§7 链接格式校验工具存在", exists("utils/link.ts"), "utils/link.ts")
    link = read("utils/link.ts")
    check("§7 链接校验能从分享文案里提取 URL",
          "extractUrl" in link and "http" in link,
          "utils/link.ts")
    check("§7 链接校验区分「缺协议头」与「不是链接」",
          "http://" in link and "没找到链接" in link,
          "utils/link.ts")
    # QuickSubmit 必须在提交前过这道闸（而不是等后端报错）
    qs = read("components/shell/QuickSubmit.vue")
    check("§7 QuickSubmit 提交前做格式校验", "checkLink" in qs, "components/shell/QuickSubmit.vue")
    check("§7 格式错误就地内联展示（role=alert）",
          "formatError" in qs and 'role="alert"' in qs,
          "components/shell/QuickSubmit.vue")
    check("§7 链接校验有独立用例", exists("utils/link.spec.ts"), "utils/link.spec.ts")

    # 提交路径必须用组件抛出的「规范化链接」，不能回头读原始 v-model。
    # 踩过的坑：下载页 onPreview 忽略 emit 的值、自己重读 url.value，
    # 于是分享文案原样进了请求（后端 400），组件里的格式闸形同虚设。
    # 注意只查「取链接那一行」——onPreview 里 url.value = ''（预览成功后清空输入框）
    # 是正确的，不能被这条断言误伤。
    tasks_view = read("views/Tasks.vue")
    m = re.search(r"async function onPreview\([^)]*\)\s*\{(.*?)\n\}", tasks_view, re.S)
    body = m.group(1) if m else ""
    check("§7 下载页提交用组件抛出的规范化链接",
          "onPreview(target" in tasks_view
          and "const link = (target ?? url.value).trim()" in body
          and "await tasks.preview(link)" in body,
          "views/Tasks.vue onPreview")
    app = read("App.vue")
    check("§7 顶栏提交用组件抛出的规范化链接",
          "onQuickSubmit(value" in app and "quickUrl.value = value" not in app,
          "App.vue onQuickSubmit")


# ---------------------------------------------------------------- §7 交互铁律
SHORTCUTS = {
    "`/` 聚焦提交框": "ev.key === '/'",
    "`Esc` 关闭/清除": "ev.key === 'Escape'",
    "`↑` 移动": "case 'ArrowUp'",
    "`↓` 移动": "case 'ArrowDown'",
    "`Enter` 详情": "case 'Enter'",
    "`Shift+Space` 多选": "ev.shiftKey",
    "`R` 重试": "case 'r'",
    "`G D` 跳转下载页": "push('/tasks')",
    "`G S` 跳转设置页": "push('/settings')",
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
    check("侧栏 Logo 用 RaccoonLogo 品牌徽标（非内联 SVG/emoji）",
          bool(brand) and "RaccoonLogo" in brand.group(1) and "<svg" not in brand.group(1),
          "App.vue .brand 块")
    # 导航图标用 SVG 线稿是 §4 的正当要求，但整页仍不许出现 emoji 冒充图标
    check("App.vue 模板无 emoji 冒充图标", not EMOJI_RE.search(
        re.search(r"<template>(.*)</template>", app, re.S).group(1) if re.search(r"<template>(.*)</template>", app, re.S) else ""
    ), "App.vue")
    # RDialog ≤480px（jsdom 不应用 scoped CSS，故在源码级断言）
    rdialog = read("components/base/RDialog.vue")
    check("§4 RDialog 宽度 ≤480px", bool(re.search(r"max-width:\s*480px", rdialog)),
          "components/base/RDialog.vue")


def check_round2_contracts() -> None:
    """第二轮（T03/T04）契约断言：全局快捷键、删文件入口、状态语义、深色描边、新端点文档。

    这些断言对应「用户真的报过的缺陷」或「docs/15 点名的契约」，全部落在
    可观测结果上（谁调了哪个接口 / 文案说了什么 / 样式有没有那条线），
    不检查「函数被定义过」这类实现细节。
    """
    # R5：快捷键必须全站可用（App.vue 装全局监听）
    app = read("App.vue")
    check("§7 全局快捷键在 App.vue 安装", "useGlobalShortcuts(" in app, "App.vue")

    # R1：删文件入口存在，且与「删任务记录」用两个不同接口
    client = read("api/client.ts")
    check("删文件接口 deleteTaskFile 存在", "deleteTaskFile:" in client, "api/client.ts")
    check("删媒体文件接口 deleteFile 存在", "deleteFile:" in client, "api/client.ts")
    check("任务详情删除产物走 deleteTaskFile", "api.deleteTaskFile" in read("views/Tasks.vue"), "views/Tasks.vue")
    check("媒体库删除文件走 deleteFile", "api.deleteFile" in read("views/Media.vue"), "views/Media.vue")
    # 删文件必须二次确认（破坏性决策，docs/07 §4）。
    # 确认框统一由 DeleteFileDialog 承担，视图（Media/Tasks）负责弹，
    # 详情面板只发事件——所以这里查的是「视图侧有确认」，不是「详情自己弹窗」。
    check("媒体库删文件有确认框", "DeleteFileDialog" in read("views/Media.vue"), "views/Media.vue")
    check("任务详情删产物有确认框（视图侧弹）",
          "DeleteFileDialog" in read("views/Tasks.vue"), "views/Tasks.vue")
    check("详情面板删文件只发事件（不自己弹窗）",
          "delete-file" in read("components/task/TaskDetail.vue")
          and "RDialog" not in read("components/task/TaskDetail.vue"),
          "components/task/TaskDetail.vue")

    # R2：删任务记录的文案不得再声称「删本地文件」（两件事已经拆开）
    tasks_view = read("views/Tasks.vue")
    check("删任务文案不再声称删本地文件", "及其本地文件" not in tasks_view, "views/Tasks.vue")

    # R9：post（后处理）没有可信百分比 → 必须走不确定进度条纹，且条纹色跟随阶段
    prog = read("components/task/TaskProgress.vue")
    check("§4 后处理阶段用条纹（不编造百分比）", "props.task.status === 'post'" in prog
          and "indeterminate" in prog, "components/task/TaskProgress.vue")
    check("§4 条纹色跟随阶段（post=success）", "--stripe: var(--success)" in prog,
          "components/task/TaskProgress.vue")

    # R10：暂停态要说明「暂停了什么、什么没变」
    check("§3 暂停订阅有文字说明", "已暂停：不再按周期检查新内容" in read("components/subs/SubList.vue"),
          "components/subs/SubList.vue")

    # R11：深色主题封面 1px 描边（深底封面与卡片底色太接近）。
    # 四主题下 night/scarf/berry 是深色画布，snow 是浅色——描边只挂深色主题。
    card = read("components/media/FileCard.vue")
    dark_themes_ok = all(t in card for t in ("night", "scarf", "berry"))
    check("§2 深色主题封面 1px 描边（night/scarf/berry）",
          dark_themes_ok and "inset 0 0 0 1px" in card,
          "components/media/FileCard.vue")

    # docs/04 路由总表必须收录本轮新增端点（文档即契约）
    api_doc = (ROOT / "docs" / "04-API详细设计.md").read_text(encoding="utf-8")
    for path in ["/system/stats/summary", "/system/stats/trend", "/system/monitor",
                 "/system/monitor/history", "/logs", "/user/preferences", "/files/{id}"]:
        check(f"docs/04 收录 {path}", path in api_doc, "docs/04-API详细设计.md")


def check_round3_ia() -> None:
    """第三轮（T05）信息架构断言：10 项导航、tabbar 5+更多、三新页、纯 SVG 图表、主页取数收敛。

    这些断言查的是「用户看到的结构」，不是「文件存不存在」：
    导航项数、底部栏槽位数、更多面板的默认隐藏规则位置、
    主页是否还在自己开一套统计轮询。
    """
    # ---- 1. 三条新路由（docs/07 §3 十条一级页面）----
    router = read("router/index.ts")
    for path in ["/history", "/logs", "/monitor"]:
        check(f"§3 路由 {path}", f"'{path}'" in router, "router/index.ts")

    # ---- 2. 侧导航 10 项 + 底部栏 5 高频 + 「更多」----
    app = read("App.vue")
    nav_count = app.count("icon: 'M")
    check("§3 侧导航 10 项", nav_count == 10, f"实际 {nav_count} 项")
    check("§3 导航项带底部栏短标签 short", app.count("short: '") == 10, f"实际 {app.count('short:')} 项")
    check("§6 底部栏 5 高频 + 「更多」",
          "TAB_ITEMS" in app and "MORE_ITEMS" in app
          and "tabbar-more" in app and "more-sheet" in app, "App.vue")
    # 层叠顺序：默认隐藏态必须写在媒体查询之前（docs/07 §8.1 ⑤ 的实测教训）
    idx_default_hide = app.find(".sheet-mask,")
    idx_mobile = app.find("@media (max-width: 767px)")
    check("§6 更多面板默认隐藏态写在媒体查询之前",
          0 <= idx_default_hide < idx_mobile, "App.vue 层叠顺序")

    # ---- 3. 主页取数收敛到 stores/system（不许页面自己再开一套统计轮询）----
    ov = read("views/Overview.vue")
    check("§2.3 主页统计来自 stores/system", "useSystemStore" in ov, "views/Overview.vue")
    check("§2.3 主页不再自己拉统计/趋势/监控",
          "api.statsSummary" not in ov and "api.statsTrend" not in ov
          and "api.monitorSnapshot" not in ov, "views/Overview.vue")
    check("§2.3 主页轮询交给 store 起停",
          "startPolling(" in ov and "stopPolling(" in ov, "views/Overview.vue")

    # ---- 4. 图表纯 SVG（禁引图表库）----
    pkg = (ROOT / "web" / "package.json").read_text(encoding="utf-8")
    for lib in ["chart.js", "echarts", "d3", "apexcharts", "recharts"]:
        check(f"§4 未引入图表库 {lib}", lib not in pkg, "package.json")
    check("§4 TrendChart 是纯 SVG（polyline）", "polyline" in read("components/charts/TrendChart.vue"),
          "components/charts/TrendChart.vue")
    check("§4 DonutChart 是纯 SVG（stroke-dasharray）",
          "stroke-dasharray" in read("components/charts/DonutChart.vue"),
          "components/charts/DonutChart.vue")
    # 诚实原则：无数据不画假线、算不出百分比不画 0%
    check("§1 图表空数据显式降级",
          "暂无数据" in read("components/charts/TrendChart.vue"), "components/charts/TrendChart.vue")
    check("§1 环形图 percent=null 显示「—」",
          "—" in read("components/charts/DonutChart.vue"), "components/charts/DonutChart.vue")

    # ---- 5. 三新页 + 各自的用例 + 外壳用例 ----
    for rel in ["views/History.vue", "views/History.spec.ts",
                "views/Logs.vue", "views/Logs.spec.ts",
                "views/Monitor.vue", "views/Monitor.spec.ts",
                "App.spec.ts"]:
        check(f"§8 {rel} 存在", exists(rel), rel)

    # ---- 6. 历史页只读：复用 TaskList 的 readonly，不出现写操作 ----
    hist = read("views/History.vue")
    check("§3 历史页复用只读列表", "readonly" in hist, "views/History.vue")
    check("§3 历史页不提供取消/重试/删除",
          "tasks.remove" not in hist and "tasks.retry" not in hist and "tasks.cancel" not in hist,
          "views/History.vue")
    for rel in ["components/task/TaskList.vue", "components/task/TaskCard.vue", "components/task/TaskRow.vue"]:
        check(f"§3 {rel} 支持 readonly", "readonly" in read(rel), rel)

    # ---- 7. 任务详情：走了哪条路 + 暂停说明（R10/T02）----
    detail = read("components/task/TaskDetail.vue")
    check("§4 详情展示 route_path", "route_path" in detail, "components/task/TaskDetail.vue")
    check("§3 详情说明「不支持暂停/续传」", "不支持暂停/续传" in detail,
          "components/task/TaskDetail.vue")

    # ---- 8. docs/07 与实现同步（文档即宪法）----
    doc07 = (ROOT / "docs" / "07-前端设计落地.md").read_text(encoding="utf-8")
    for path in ["/history", "/logs", "/monitor"]:
        check(f"docs/07 §3 收录 {path}", path in doc07, "docs/07-前端设计落地.md")
    for comp in ["MetricCard", "TrendChart", "DonutChart", "DeleteFileDialog"]:
        check(f"docs/07 §4 收录 {comp}", comp in doc07, "docs/07-前端设计落地.md")
    check("docs/07 §6 记录 tabbar 5+更多", "5 高频 + 「更多」" in doc07, "docs/07-前端设计落地.md")
    check("docs/07 §2 surface-2 是合法 hex", "#EEEF2;" not in doc07, "docs/07-前端设计落地.md")


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
    # §3 点名的视图级组件必须有自己的用例（组件边界落地 = 有独立验收点）
    view_comp_specs = {
        "SetupWizard.spec.ts", "QuickSubmit.spec.ts", "StatusStrip.spec.ts",
        "ActiveTasks.spec.ts", "IssuesPanel.spec.ts", "TaskList.spec.ts",
        "FileCard.spec.ts", "PreviewDrawer.spec.ts", "DownloadBtn.spec.ts",
        "SubList.spec.ts", "SubForm.spec.ts", "ItemsTable.spec.ts",
        "SourceList.spec.ts", "RecordPlanForm.spec.ts", "RecordingList.spec.ts",
        "CredentialList.spec.ts", "QbTestPanel.spec.ts", "TelegramPanel.spec.ts",
        "SettingsNav.spec.ts", "SettingItem.spec.ts", "DiagPanel.spec.ts",
    }
    have = set(comp_specs)
    missing_comp = sorted(view_comp_specs - have)
    check("§3 视图级组件都有独立用例（组件边界有验收点）", not missing_comp,
          f"缺 spec：{missing_comp}" if missing_comp else "")


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
          and ".nav-txt { display: none; }" in app and ".nav-sec-label { display: none; }" in app)
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
    # 底部栏每个入口都要有可读标签文本（不能是空 span）。
    # 10 项导航后底部栏改用 short 短标签（「媒体库」在 375px 的 52px 槽位里放不下，
    # docs/15 §1.2 裁决 5），所以这里两种取值都认——但必须是真文本而不是空。
    check("§6 <768 底部栏渲染 label 文本",
          "tabbar-label" in app and re.search(
              r'<span class="tabbar-label">\{\{ item\.(label|short) \}\}</span>', app) is not None,
          "App.vue tabbar 模板")


def check_page_composition() -> None:
    """页面构图层的验收点（§8.1 ①「存在性 ≠ 像个产品」的盲区）。

    背景：零件级断言可以全绿——组件在、被视图引用、有独立 spec——
    但页面照样能塌，而且塌的地方只有「渲染出来是个页面」才看得见：

    - IssuesPanel 无失败任务时整块消失 → 总览页首屏空半屏（§7 明令不展示空白页）；
    - 媒体库非图片占位直接渲染英文 kind 原词（video/audio）→ 观感就是没做完；
    - 侧导航用户区和上面 15 个导航项无分隔 → 像一条没写完的列表。

    所以闸门要查构图，不只查零件。
    """
    # ① 总览 IssuesPanel：无失败时必须有正面态，不许整块消失
    issues = read("components/overview/IssuesPanel.vue")
    check("§7 IssuesPanel 无失败时有正面态（总览页不留结构空洞）",
          "v-else" in issues and "ok-panel" in issues and "没有失败的任务" in issues,
          "components/overview/IssuesPanel.vue")
    check("§5 IssuesPanel 正面态用 done 表情（按状态取用）",
          'face="done"' in issues,
          "components/overview/IssuesPanel.vue")

    # ② 媒体库占位：非图片不许扔英文 kind 原词
    card = read("components/media/FileCard.vue")
    check("§4 媒体库占位不渲染英文 kind 原词",
          "{{ file.kind }}" not in card and "kindText" in card,
          "components/media/FileCard.vue")
    check("§4 媒体库占位有类型 SVG 图标（禁 emoji 冒充）",
          "file-kind-icon" in card and "<svg" in card,
          "components/media/FileCard.vue")
    check("§4 媒体库占位底色注入平台品牌色（网格里可辨认）",
          "platformTone" in card and "color-mix" in card,
          "components/media/FileCard.vue")

    # ③ 侧导航用户区与导航项之间必须有分隔线
    app = read("App.vue")
    foot = app[app.find(".nav-foot"):app.find(".nav-foot") + 400]
    check("§6 侧导航用户区与导航项有分隔线（不成无尽列表）",
          "border-top" in foot,
          "App.vue .nav-foot")

    # ④ 订阅计数徽章要有淡色底（四个计数不能糊成一片）
    sub = read("components/subs/SubList.vue")
    cnt = sub[sub.find(".cnt {"):sub.find(".cnt {") + 700]
    check("§4 订阅计数徽章有淡色底（分群可读）",
          "color-mix" in cnt and cnt.count("background") >= 4,
          "components/subs/SubList.vue .cnt")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args()

    check_routes()
    check_components()
    check_view_components()
    check_view_component_usage()
    check_settings_nav()
    check_setting_item()
    check_diag_entry()
    check_setup_wizard()
    check_engine_detail_visible()
    check_link_validation()
    check_page_composition()
    check_visual_direction()
    check_tokens()
    check_typography()
    check_typography_semantics()
    check_no_hardcoded_colors()
    check_source_integrity()
    check_grid_collapse()
    check_no_emoji_icons()
    check_face_system()
    check_shortcuts()
    check_layout_fixes()
    check_round2_contracts()
    check_round3_ia()
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
