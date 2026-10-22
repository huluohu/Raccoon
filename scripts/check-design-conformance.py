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


def check_face_system() -> None:
    """§5 六表情：RaccoonFace 需在空态（REmpty）被使用。"""
    rempty = read("components/base/REmpty.vue")
    check("§5 RaccoonFace 接入空态 REmpty", "RaccoonFace" in rempty, "components/base/REmpty.vue")
    rface = read("components/raccoon/RaccoonFace.vue")
    faces = ["waiting", "working", "done", "failed", "cancelled", "surprise"]
    missing = [f for f in faces if f"'{f}'" not in rface]
    check("§5 六表情齐备", not missing, f"缺：{missing}" if missing else "")


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


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args()

    check_routes()
    check_components()
    check_tokens()
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
