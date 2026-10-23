<script setup lang="ts">
// 运行日志页（docs/15 T05 验收 #5）：级别 chips + module 下拉 + 关键词 + 时间范围
// + cursor 分页 + E4 实时跟随（3s，默认关）。
//
// 设计取舍（为什么这么做）：
// - msg 已在服务端统一脱敏（docs/15 §6.3：出口只有 server.redactLine 一套），
//   前端原样等宽展示，**不做任何还原/二次加工**——前端「补全」脱敏串等于把密钥写回页面。
// - 筛选一变就回到第一页（cursor 归零）：cursor 是行号游标，拿着旧游标配新条件，
//   取到的是「另一批结果里第 N 行之后」，行号对不上就会漏行。
// - 实时跟随整体替换而不是追加：每 3 秒拉最新一页，追加会与服务端窗口重叠出重复行。
// - 上翻超过一屏即自动停跟：用户正在读历史时被新日志顶走是最恼人的体验；
//   停跟后浮出「回到最新」，由用户决定什么时候回到尾部。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { LogItem } from '@/api/types'
import { errorText, formatTime } from '@/utils/format'
import RButton from '@/components/base/RButton.vue'
import REmpty from '@/components/base/REmpty.vue'

/** 单页条数：接口上限 500，取 200——一屏约 20 行，200 条足够翻十屏且响应体不至于太大。 */
const PAGE_SIZE = '200'
/** 关键词防抖：每敲一个字都打接口会让结果闪烁，也会把后端日志解析压满。 */
const DEBOUNCE_MS = 300
/** E4 跟随间隔（docs/15 §6.5：logs 跟随 3s）。 */
const FOLLOW_MS = 3000
/** 「离开底部」判定容差：40px 吸收惯性滚动与半行高度，避免一碰滚轮就误判上翻。 */
const BOTTOM_SLACK = 40

const LEVELS = [
  { value: 'debug', label: 'debug' },
  { value: 'info', label: 'info' },
  { value: 'warn', label: 'warn' },
  { value: 'error', label: 'error' },
] as const

type RangeKey = 'all' | '1h' | '24h'
const RANGES: { value: RangeKey; label: string }[] = [
  { value: 'all', label: '不限' },
  { value: '1h', label: '近 1 小时' },
  { value: '24h', label: '近 24 小时' },
]

// ---- 筛选状态 ----
const levels = ref<string[]>([]) // 空数组 = 不限级别
const moduleFilter = ref('')
const keyword = ref('')
/** 防抖后的关键词：真正进请求参数的是它，不是输入框里的实时值。 */
const debouncedKeyword = ref('')
const range = ref<RangeKey>('all')

// ---- 数据状态 ----
const items = ref<LogItem[]>([])
/** 下拉选项来源：累积「出现过的 module」。只增不删——否则筛到某个模块后
 *  列表一变，当前选中项就从下拉里消失，控件显示与筛选条件对不上。 */
const modules = ref<string[]>([])
const loading = ref(true)
const loadingMore = ref(false)
const errorMsg = ref('')
/** 本页请求用的 cursor（用于判断 next_cursor 是否前进）。 */
const cursor = ref('')
const nextCursor = ref('')

// ---- E4 实时跟随 ----
const follow = ref(false)
/** 因上翻自动停跟：只有这种情况下才浮出「回到最新」。 */
const stoppedByScroll = ref(false)

const bodyEl = ref<HTMLElement | null>(null)
let followTimer: number | undefined
let keywordTimer: number | undefined

const moduleOptions = computed(() => [
  { value: '', label: '全部模块' },
  ...modules.value.map((m) => ({ value: m, label: m })),
])

/**
 * 还有更多吗？服务端约定：没有下一页时 next_cursor 原样返回请求的 cursor
 * （见 api/types.ts 的 LogPage 注释）。首屏请求 cursor 为空，故空游标也等于「到底了」。
 */
const hasMore = computed(() => nextCursor.value !== '' && nextCursor.value !== cursor.value)

/** 时间范围 → from（Unix 秒）。按「现在减窗口」算，不写死时间戳常量。 */
function rangeFrom(r: RangeKey): string {
  if (r === 'all') return ''
  const windowSec = r === '1h' ? 3600 : 86400
  return String(Math.floor(Date.now() / 1000) - windowSec)
}

/** 组装查询参数：空值一律不下发，避免后端把空串当有效过滤条件。 */
function baseParams(): Record<string, string> {
  const p: Record<string, string> = { limit: PAGE_SIZE }
  if (levels.value.length) p.level = levels.value.join(',')
  if (moduleFilter.value) p.module = moduleFilter.value
  const q = debouncedKeyword.value.trim()
  if (q) p.q = q
  const from = rangeFrom(range.value)
  if (from) p.from = from
  return p
}

function rememberModules(list: LogItem[]) {
  const merged = new Set(modules.value)
  for (const it of list) if (it.module) merged.add(it.module)
  modules.value = [...merged].sort((a, b) => a.localeCompare(b))
}

async function scrollToBottom() {
  await nextTick()
  const el = bodyEl.value
  if (el) el.scrollTop = el.scrollHeight
}

/**
 * 拉第一页并整体替换。
 * @param quiet 跟随轮询用：不切 loading（否则每 3 秒闪一次骨架）。
 */
async function loadFirst(quiet = false) {
  if (!quiet) loading.value = true
  try {
    const res = await api.logs(baseParams())
    items.value = res.items ?? []
    rememberModules(items.value)
    cursor.value = ''
    nextCursor.value = res.next_cursor ?? ''
    errorMsg.value = ''
    if (follow.value) await scrollToBottom()
  } catch (e) {
    errorMsg.value = errorText(e, '日志拉取失败')
  } finally {
    if (!quiet) loading.value = false
  }
}

/** 加载更多：带上一页的 next_cursor，结果**追加**到末尾。 */
async function loadMore() {
  const reqCursor = nextCursor.value
  loadingMore.value = true
  try {
    const res = await api.logs({ ...baseParams(), cursor: reqCursor })
    items.value = [...items.value, ...(res.items ?? [])]
    rememberModules(res.items ?? [])
    cursor.value = reqCursor
    nextCursor.value = res.next_cursor ?? ''
    errorMsg.value = ''
  } catch (e) {
    errorMsg.value = errorText(e, '加载更多失败')
  } finally {
    loadingMore.value = false
  }
}

function toggleLevel(v: string) {
  levels.value = levels.value.includes(v) ? levels.value.filter((x) => x !== v) : [...levels.value, v]
}

// ---- 实时跟随 ----
function stopFollow() {
  follow.value = false
  if (followTimer) {
    window.clearInterval(followTimer)
    followTimer = undefined
  }
}

function startFollow() {
  if (followTimer) window.clearInterval(followTimer)
  // 立刻拉一次：开关打开后到第一拍之间不该有 3 秒空窗。
  void loadFirst(true)
  followTimer = window.setInterval(() => {
    // 页面不可见时不打接口（docs/15 §6.5）：后台标签页里轮询只是白烧 CPU 和日志。
    if (document.hidden) return
    void loadFirst(true)
  }, FOLLOW_MS)
}

/** 容器滚动：离底超过一屏判定为「上翻」，此时自动停跟。 */
function onScroll() {
  const el = bodyEl.value
  if (!el) return
  const awayFromBottom = el.scrollTop < el.scrollHeight - el.clientHeight - BOTTOM_SLACK
  if (awayFromBottom && follow.value) {
    stopFollow()
    stoppedByScroll.value = true
  }
}

/** 「回到最新」：恢复跟随并滚到底部。 */
async function backToLatest() {
  stoppedByScroll.value = false
  follow.value = true
  await scrollToBottom()
}

watch(follow, (on) => {
  if (on) {
    stoppedByScroll.value = false
    startFollow()
  } else if (followTimer) {
    window.clearInterval(followTimer)
    followTimer = undefined
  }
})

watch(keyword, (v) => {
  if (keywordTimer) window.clearTimeout(keywordTimer)
  keywordTimer = window.setTimeout(() => (debouncedKeyword.value = v), DEBOUNCE_MS)
})

// 筛选变化 → 回第一页。level/module/时间范围立即生效（点选是明确动作，不需要防抖）。
watch([debouncedKeyword, moduleFilter, range, levels], () => void loadFirst())

onMounted(() => void loadFirst())

// 定时器必须在卸载时清掉：页面切走后回调仍会改已卸载组件的 ref，也会继续打接口。
onBeforeUnmount(() => {
  if (followTimer) window.clearInterval(followTimer)
  if (keywordTimer) window.clearTimeout(keywordTimer)
  followTimer = undefined
  keywordTimer = undefined
})
</script>

<template>
  <section class="logs">
    <header class="page-head">
      <h1>运行日志</h1>
      <span class="count tnum">已加载 {{ items.length }} 条</span>
    </header>

    <div class="toolbar">
      <div class="chips" role="group" aria-label="日志级别（可多选）">
        <button
          v-for="lv in LEVELS"
          :key="lv.value"
          type="button"
          class="chip"
          :class="{ on: levels.includes(lv.value) }"
          :aria-pressed="levels.includes(lv.value)"
          @click="toggleLevel(lv.value)"
        >
          {{ lv.label }}
        </button>
      </div>

      <select id="log-module" v-model="moduleFilter" class="select" aria-label="模块">
        <option v-for="o in moduleOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
      </select>

      <input
        id="log-q"
        v-model="keyword"
        class="search"
        type="search"
        maxlength="256"
        aria-label="关键词"
        placeholder="搜索关键词，如 COOKIE_EXPIRED"
      />

      <div class="chips" role="group" aria-label="时间范围">
        <button
          v-for="r in RANGES"
          :key="r.value"
          type="button"
          class="chip"
          :class="{ on: range === r.value }"
          :aria-pressed="range === r.value"
          @click="range = r.value"
        >
          {{ r.label }}
        </button>
      </div>

      <label class="follow">
        <input v-model="follow" type="checkbox" />
        <span>实时跟随</span>
        <span class="follow-state" :class="{ live: follow }">{{ follow ? '跟随中 · 3 秒' : '已暂停' }}</span>
      </label>
    </div>

    <p v-if="errorMsg" class="err">
      <span>{{ errorMsg }}</span>
      <RButton size="sm" variant="outline" @click="loadFirst()">重试</RButton>
    </p>

    <div class="log-wrap">
      <div ref="bodyEl" class="log-body" @scroll="onScroll">
        <ol v-if="items.length" class="log-list">
          <li v-for="(it, i) in items" :key="`${it.ts}-${i}`" class="log-row">
            <time class="ts tnum">{{ formatTime(it.ts * 1000) }}</time>
            <span class="lv-badge" :class="`lv-badge--${it.level}`">{{ it.level }}</span>
            <span class="mod">{{ it.module }}</span>
            <!-- msg 已脱敏，原样输出；等宽 + 可换行，长日志不撑破页面 -->
            <code class="msg">{{ it.msg }}</code>
          </li>
        </ol>

        <REmpty
          v-else-if="!loading && !errorMsg"
          face="waiting"
          title="这个筛选下没有日志"
          hint="日志文件为空时这里是空的——服务跑起来写第一条日志就会出现"
        />

        <p v-else-if="loading" class="hint">加载中…</p>

        <p v-if="!hasMore && items.length" class="hint">没有更多日志</p>

        <div v-if="hasMore" class="more">
          <RButton size="sm" variant="outline" :disabled="loadingMore" @click="loadMore">
            {{ loadingMore ? '加载中…' : '加载更多' }}
          </RButton>
        </div>
      </div>

      <button v-if="stoppedByScroll" type="button" class="jump" @click="backToLatest">回到最新</button>
    </div>
  </section>
</template>

<style scoped>
.logs {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.page-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}
.page-head h1 {
  font-size: var(--font-title-2);
  font-weight: var(--weight-title);
  color: var(--text-1);
}
.count {
  font-size: var(--font-caption);
  color: var(--text-3);
}
.toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.chips {
  display: inline-flex;
  gap: 6px;
}
.chip {
  padding: 6px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  background: var(--surface);
  color: var(--text-2);
  font-size: var(--font-aux);
  cursor: pointer;
  transition:
    background var(--motion-micro) var(--motion-micro-ease),
    color var(--motion-micro) var(--motion-micro-ease);
}
/* 选中态不只靠底色：aria-pressed 同步给读屏，视觉上还有描边加重 */
.chip.on {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--on-accent);
}
.select,
.search {
  padding: 6px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--surface);
  color: var(--text-1);
  font-size: var(--font-aux);
}
.search {
  flex: 1;
  min-width: 180px;
}
.select:focus-visible,
.search:focus-visible,
.chip:focus-visible,
.jump:focus-visible {
  outline: 2px solid var(--info);
  outline-offset: 1px;
}
.follow {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--font-aux);
  color: var(--text-2);
  cursor: pointer;
}
/* 开关状态有文字说明（跟随中 / 已暂停），不靠颜色单独表达 */
.follow-state {
  font-size: var(--font-aux);
  color: var(--text-3);
}
.follow-state.live {
  color: var(--success);
}
.err {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 0;
  padding: 10px 14px;
  border: 1px solid var(--danger);
  border-radius: var(--radius-card);
  background: var(--surface);
  color: var(--danger);
  font-size: var(--font-aux);
}
.log-wrap {
  position: relative;
}
/* 限高 + 内部滚动：日志再长也不会把整页撑成几万像素 */
.log-body {
  max-height: 62vh;
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--surface);
  padding: 8px 12px;
}
.log-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.log-row {
  display: grid;
  grid-template-columns: max-content max-content max-content minmax(0, 1fr);
  align-items: baseline;
  gap: 10px;
  padding: 4px 0;
  border-bottom: 1px solid var(--border);
}
.log-row:last-child {
  border-bottom: none;
}
/* 时间戳 / 模块名：要读的字，用辅助档 13.5px。
   12px(caption) 只留给表头与徽章——级别胶囊 .lv-badge 与计数 .count 才是徽章。 */
.ts {
  font-size: var(--font-aux);
  color: var(--text-3);
}
/* 级别徽章：色 + 文字双编码（docs/07 §8.2 颜色不是唯一载体） */
.lv-badge {
  padding: 0 6px;
  border: 1px solid currentcolor;
  border-radius: var(--radius-pill);
  font-size: var(--font-caption);
  font-weight: var(--weight-caption);
  text-transform: lowercase;
}
.lv-badge--debug {
  color: var(--text-3);
}
.lv-badge--info {
  color: var(--text-2);
}
.lv-badge--warn {
  color: var(--warning);
}
.lv-badge--error {
  color: var(--danger);
}
.mod {
  font-size: var(--font-aux);
  color: var(--text-2);
}
/* 等宽：日志原文的缩进/对齐在比例字体里会散架。
   font-family 不在 token 白名单内（token 只管色值/字号/圆角/阴影/动效），故直接写系统等宽栈。 */
.msg {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: var(--font-aux);
  color: var(--text-1);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.hint {
  margin: 12px 0;
  text-align: center;
  font-size: var(--font-aux);
  color: var(--text-3);
}
.more {
  display: flex;
  justify-content: center;
  padding: 12px 0;
}
/* 浮在列表右下角：停跟后随时能回到尾部，不占列表高度 */
.jump {
  position: absolute;
  right: 16px;
  bottom: 16px;
  padding: 8px 14px;
  border: 1px solid var(--accent);
  border-radius: var(--radius-pill);
  background: var(--surface);
  color: var(--accent);
  font-size: var(--font-aux);
  box-shadow: var(--shadow-2);
  cursor: pointer;
}
</style>
