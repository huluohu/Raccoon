<script setup lang="ts">
// TaskDetail：任务详情面板（docs/07 §4「概览/进度/日志/错误与建议/操作五区；Cookie 过期 → 一键跳 connections」）。
//
// 为什么这么设计：
// 1) 五个区块固定分区、区间用细分割线隔开（对齐 base/DetailDrawer 的分隔风格）：
//    详情是抽屉里信息密度最高的地方，区块不分明读者就得自己在里面找。
// 2) 组件不自己 fetch：task / events / files 全部由上层传入。
//    组件里发请求会让「详情数据来自哪里」变成第二份真相（SSE 打补丁后组件内的旧副本不会更新）。
// 3) 错误区讲「事实 + 下一步」：错误码（可检索）、原因（服务端原话）、建议（下一步能做什么）。
//    建议优先用服务端下发的 fix_hint（它知道引擎与凭据的真实状态），前端只做兜底文案。
// 4) Cookie / 登录失效必须给一条真出口：emit('goto-connections')，由上层跳路由——
//    组件里不写死路由，视图换了地址不用回来改组件。
import { computed, ref } from 'vue'
import { api } from '@/api/client'
import StatusDot from './StatusDot.vue'
import TaskProgress from './TaskProgress.vue'
import PlatformBadge from './PlatformBadge.vue'
import RButton from '@/components/base/RButton.vue'
import RaccoonFace from '@/components/raccoon/RaccoonFace.vue'
import type { Task, TaskEvent, TaskFile } from '@/api/types'
import { formatBytes, formatSpeed, formatTime, WAIT_REASON_TEXT } from '@/utils/format'
import { useNowTick } from '@/utils/now'

const props = withDefaults(
  defineProps<{
    /** 任务本体（字段定义见 api/types.ts）。 */
    task: Task
    /** 事件日志（上层拉取后传入，组件不自己 fetch）。 */
    events?: TaskEvent[]
    /** 产物文件（可选，没有就不渲染这行）。 */
    files?: TaskFile[]
    /** 操作进行中：把操作按钮置灰，避免重复提交。 */
    busy?: boolean
  }>(),
  { events: () => [], files: () => [], busy: false },
)

const emit = defineEmits<{
  /** Cookie / 登录失效 → 跳「连接」页更新凭据（路由由上层处理）。 */
  (e: 'goto-connections'): void
  /** 引擎 / 网络配置问题 → 跳「设置」页。 */
  (e: 'goto-settings'): void
  /** 重试任务。 */
  (e: 'retry', task: Task): void
  /** 换通道重试（kind='n115'：本地下载失败转 115 离线，docs/05 §7）。 */
  (e: 'retry-via', task: Task, kind: 'n115'): void
  /** 切换代理重试（直连超时或受限时手动尝试走代理）。 */
  (e: 'retry-proxy', task: Task): void
  /** 取消任务。 */
  (e: 'cancel', task: Task): void
  /** 删除任务。 */
  (e: 'remove', task: Task): void
  /** 删除单个产物文件（确认后发出；真正的请求由上层执行，组件不 fetch）。 */
  (e: 'delete-file', file: TaskFile): void
}>()

defineSlots<{
  /** 操作区（覆盖默认按钮）。 */
  actions(props: { task: Task }): unknown
  /** 概览区附加内容。 */
  'overview-extra'(props: { task: Task }): unknown
  /** 日志区附加内容。 */
  'logs-extra'(props: { task: Task }): unknown
}>()

/** 服务端 fix_hint 的形状：{action,label}（backend internal/task/worker.go）。 */
interface FixHint {
  action: string
  label: string
}

/** 解析服务端 fix_hint；解析失败（老数据 / 非 JSON）不报错，退回按错误码给建议。 */
const fixHint = computed<FixHint | null>(() => {
  const raw = props.task.fix_hint
  if (!raw) return null
  try {
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== 'object' || parsed === null) return null
    const obj = parsed as { action?: unknown; label?: unknown }
    const action = typeof obj.action === 'string' ? obj.action : ''
    const label = typeof obj.label === 'string' ? obj.label : ''
    if (!action && !label) return null
    return { action, label }
  } catch {
    return null
  }
})

/** 是否需要更新凭据：fix_hint 指路、错误码是 AUTH_REQUIRED、或在等登录。 */
const needsCredentials = computed(() => {
  if (fixHint.value?.action === 'goto_credentials') return true
  if (props.task.error_code === 'AUTH_REQUIRED') return true
  if (props.task.status === 'waiting' && props.task.wait_reason === 'need_login') return true
  return false
})

/** 是否需要跳设置：fix_hint 指路或引擎不可用。 */
const needsSettings = computed(() => {
  if (fixHint.value?.action === 'goto_settings') return true
  return props.task.error_code === 'ENGINE_UNAVAILABLE'
})

/** 重试等待期：任务因可重试错误退避中（waiting+auto_retry）。此时任务没终态化，
 *  错误区要显示「第 N 次没成 + 倒计时」，而不是按终态失败渲染。 */
const isAutoRetry = computed(
  () => props.task.status === 'waiting' && props.task.wait_reason === 'auto_retry',
)

/** 错误区是否值得展示：真失败 / 有错误码（含重试等待期）/ 缺凭据。 */
const showError = computed(
  () => props.task.status === 'failed' || !!props.task.error_code || !!props.task.error_message || needsCredentials.value,
)

/** 可重试：失败/已取消可手动重来；退避等待期可「立即重试」（docs/05 §7）。 */
const canRetry = computed(
  () => props.task.status === 'failed' || props.task.status === 'cancelled' || isAutoRetry.value,
)

const nowTick = useNowTick()

/** 退避剩余秒数：scheduled_at 未到点时给正数；到点未领取（worker 占满）显示 0。 */
const retrySeconds = computed(() => {
  if (!isAutoRetry.value || !props.task.scheduled_at) return 0
  return Math.max(0, Math.ceil((props.task.scheduled_at - nowTick.value) / 1000))
})

/** 重试按钮文案：退避期强调「不用等」。 */
const retryLabel = computed(() => (isAutoRetry.value ? '不等了，立即重试' : '重试这个任务'))

/**
 * 可换道 115：本地下载任务 + 网络类错误 + 未终态成功。
 * 115 离线的抓取发生在云端（另一条网络路径），正是「本机连不上媒体流」的出路；
 * 平台类错误（登录/404/清晰度）换道无意义，不拿按钮骚扰用户。
 * 未配置 115 时后端会 409 如实拒绝，由上层 toast 转述——按钮不预判配置状态。
 */
const REROUTE_ERROR_CODES = new Set(['NET_TIMEOUT', 'HTTP_5XX', 'TIMEOUT', 'RATE_LIMITED'])
const canReroute115 = computed(
  () =>
    REROUTE_ERROR_CODES.has(props.task.error_code ?? '') &&
    props.task.kind === 'download' &&
    (props.task.status === 'failed' || isAutoRetry.value),
)

/**
 * 删文件：组件只发事件，确认框由上层（Tasks.vue 的 DeleteFileDialog）统一负责。
 * 详情面板保持「不 fetch、不弹窗」的纯展示定位，破坏性决策只有一处实现。
 */
function requestDeleteFile(f: TaskFile) {
  emit('delete-file', f)
}

/** 活跃任务可以取消。 */
const isActive = computed(() =>
  ['waiting', 'parsing', 'downloading', 'post'].includes(props.task.status),
)

/** 错误码 → 下一步建议（服务端 fix_hint 缺位时的兜底；文案只讲事实和下一步）。 */
const ERROR_SUGGESTION: Record<string, string> = {
  AUTH_REQUIRED: '去「连接」页更新该平台的凭据，再重试一次',
  FORMAT_UNAVAILABLE: '换一个清晰度再试，这个档位这次没取到',
  NOT_FOUND: '链接可能已删除或不可见，换个链接试试',
  PARSE_FAILED: '页面结构变了或链接不完整，重新复制一次链接',
  NET_TIMEOUT: '网络超时，等一会儿重试；反复超时可到「设置 → 服务」配置网络代理，或在「连接」页改走 115 离线通道',
  HTTP_4XX: '对方拒绝了这次请求，确认链接与凭据是否还有效',
  HTTP_5XX: '对方服务临时不可用，稍后重试',
  RATE_LIMITED: '请求太频繁，歇一会儿再试',
  GEO_BLOCKED: '该内容有地区限制，换个来源试试',
  ENGINE_UNAVAILABLE: '去「设置」页检查引擎是否安装、版本是否匹配',
  ENGINE_CRASH: '引擎进程异常退出，重试一次；反复出现就去「设置」页看引擎版本',
  ENGINE_ERROR: '引擎返回了错误信息，重试或换清晰度',
  WORKER_PANIC: '内部异常，任务已重新排队',
  DISK_LOW: '磁盘余量不足，先清理空间再下载',
  QB_UNAVAILABLE: 'qb 客户端连不上，去「连接」页测一下通道',
  QB_SUBMIT_FAILED: 'qb 拒绝了这单，去「连接」页确认客户端与种子配置',
  QB_QUERY_FAILED: '查不到 qb 上的任务，去「连接」页测一下通道',
  QB_FAILED: 'qb 侧出错，去「连接」页测一下通道',
  N115_UNAVAILABLE: '115 通道连不上，去「连接」页测一下通道',
  N115_SUBMIT_FAILED: '115 拒绝了这单，去「连接」页确认凭据',
  N115_QUERY_FAILED: '查不到 115 上的任务，去「连接」页测一下通道',
  N115_FAILED: '115 侧出错，去「连接」页测一下通道',
  NO_OUTPUT: '引擎跑完了但没产出文件，换清晰度或换链接重试',
}

/**
 * 下一步建议：服务端 fix_hint 优先（它知道引擎 / 凭据的真实状态），
 * 没有就按错误码给一条能直接照做的建议。
 */
const suggestion = computed(() => {
  if (fixHint.value?.label) return fixHint.value.label
  const code = props.task.error_code ?? ''
  if (ERROR_SUGGESTION[code]) return ERROR_SUGGESTION[code]
  if (needsCredentials.value) return '去「连接」页更新该平台的凭据，再重试一次'
  return '先重试一次；同样的错再来，就该换个思路（换清晰度 / 换链接 / 检查引擎）了'
})

/** 失败原因：服务端原话优先；缺凭据时用等待原因说明；都没有就如实说「没给原因」。 */
const errorReason = computed(() => {
  if (props.task.error_message) return props.task.error_message
  if (needsCredentials.value && props.task.wait_reason) {
    return WAIT_REASON_TEXT[props.task.wait_reason] ?? '需要登录才能继续'
  }
  return '服务端没有给出原因'
})

/**
 * 引擎原始报错（yt-dlp / gallery-dl 的 ERROR 行）。
 *
 * 为什么把它从日志区提到错误区：
 * error_message 是分类后的中文文案（"所选清晰度不可用"），
 * 说的是「属于哪一类问题」；引擎原文说的是「到底报了什么」
 * （"Requested format is not available"）。
 * 只给前者，用户换清晰度、换链接还是升级引擎全靠猜；
 * 两者都给，才谈得上「讲事实 + 给下一步」。
 *
 * 取法：后端把引擎 stderr 摘要写进 event='engine_error' 的事件里（backend worker.go），
 * 这里从事件流里取最后一条。
 */
const engineDetail = computed(() => {
  const hits = props.events.filter((e) => e.event === 'engine_error' && e.message)
  return hits.length ? hits[hits.length - 1].message : ''
})

/**
 * 错误结论：把「失败」翻译成一句人话。
 * 讲事实 + 小机灵鬼式幽默，但整屏只此一处拟人（docs/07 §5「每界面至多一处拟人」），其余区块保持中性。
 */
const conclusion = computed(() => {
  const code = props.task.error_code ?? ''
  // 重试等待期：不是终态失败，讲「第几次没成 + 什么时候再试」，语气放平。
  if (isAutoRetry.value) {
    const tail = retrySeconds.value > 0 ? `${retrySeconds.value} 秒后自动再试` : '马上自动再试'
    return `第 ${props.task.attempt + 1} 次没搬成（${code}），${tail}。`
  }
  // 只是「在等」而不是失败（例如缺凭据在排队）时不说「这单没成」，避免讲错事实。
  if (!code && props.task.status !== 'failed') return ''
  switch (code) {
    case 'AUTH_REQUIRED':
      return '这单没成：对方不放行，得先递个名帖。'
    case 'NOT_FOUND':
      return '这单没成：门牌号对不上，链接大概已经拆了。'
    case 'FORMAT_UNAVAILABLE':
      return '这单没成：这档画质没开门，换一档试试。'
    case 'NET_TIMEOUT':
      return '这单没成：路不通，掉头再来一次。'
    case 'HTTP_5XX':
      return '这单没成：对方那边正闹脾气，稍后再来。'
    case 'RATE_LIMITED':
      return '这单没成：门卫嫌太频繁，歇会儿再来。'
    case 'GEO_BLOCKED':
      return '这单没成：这道门只开给本地邮编。'
    case 'ENGINE_UNAVAILABLE':
    case 'ENGINE_CRASH':
    case 'ENGINE_ERROR':
      return '这单没成：工具箱没打开，得先修工具。'
    default:
      return '这单没成。'
  }
})

const title = computed(() => props.task.title || props.task.url)

/**
 * 缩略图地址：走服务端代理（去黑边 + 缩放 + 缓存），不直连平台 CDN。
 * 代理端点在服务端不可用时（老版本镜像）自动回退到原始直链。
 */
const thumbUrl = computed(() => {
  if (!props.task.thumbnail) return ''
  return api.taskThumbnailUrl(props.task.id)
})

/** 引擎与版本合成一行（没引擎时显示占位，不留空）。 */
const engineText = computed(() => {
  const engine = props.task.engine || '—'
  return props.task.engine_version ? `${engine} ${props.task.engine_version}` : engine
})

/** 已下载 / 预期大小（预期未知时只显示已下载，不猜）。 */
const sizeText = computed(() => {
  const exp = props.task.expected_size
  const dl = formatBytes(props.task.downloaded_size)
  if (!exp) return dl
  return `${dl} / ${formatBytes(exp)}`
})

/** 产物文件名（取路径末段）。 */
function fileName(f: TaskFile): string {
  const parts = f.path.split('/')
  return parts[parts.length - 1] || f.path
}

const generatingSummary = ref(false)

async function onGenerateTaskSummary() {
  generatingSummary.value = true
  try {
    const res = await api.taskSummary(props.task.id)
    props.task.summary = res.summary
  } catch (e: unknown) {
    props.task.summary = '生成失败: ' + (e instanceof Error ? e.message : '请在设置页检查 AI 配置')
  } finally {
    generatingSummary.value = false
  }
}
</script>

<template>
  <div class="tdetail">
    <!-- ① 概览 -->
    <section class="td-sec">
      <h3 class="td-sec-title">概览</h3>
      <!-- 缩略图：任务详情此前完全没有封面，与列表/媒体库不一致（同一份内容三种形态）。 -->
      <img
        v-if="thumbUrl"
        class="td-thumb"
        :src="thumbUrl"
        :alt="title"
        loading="lazy"
        referrerpolicy="no-referrer"
      />
      <p class="td-title" :title="title">{{ title }}</p>
      <dl class="td-kv">
        <dt>状态</dt>
        <dd><StatusDot :status="task.status" /></dd>
        <dt>平台</dt>
        <dd><PlatformBadge :platform="task.platform" /></dd>
        <dt>清晰度</dt>
        <dd class="tnum">{{ task.quality }}</dd>
        <dt>引擎</dt>
        <dd>{{ engineText }}</dd>
        <!-- 走了哪条路（docs/15 §3.7）：磁力/种子任务可能经历「115 优先 → 转 qB」，
             只显示最终结果而不显示路径，用户永远搞不清为什么在 qB 里。 -->
        <dt>走了哪条路</dt>
        <dd>{{ task.route_path || '—' }}</dd>
        <template v-if="task.proxy">
          <dt>网络代理</dt>
          <dd class="td-path">{{ task.proxy }}</dd>
        </template>
        <!-- 媒体直链域名：解析打的是站点 API，下载打的是媒体 CDN（两者常不是同一个域名）。
             把域名摊开，「解析成功却下载超时」才看得见该查哪一段路。 -->
        <template v-if="task.media_host">
          <dt>媒体直链域名</dt>
          <dd class="td-path">{{ task.media_host }}</dd>
        </template>
        <template v-if="task.media_url">
          <dt>媒体直链</dt>
          <dd class="td-path td-media-url" :title="task.media_url">{{ task.media_url }}</dd>
        </template>
        <dt>已下载</dt>
        <dd class="tnum">{{ formatBytes(task.downloaded_size) }}</dd>
        <dt>创建</dt>
        <dd class="tnum">{{ formatTime(task.created_at) }}</dd>
        <template v-if="task.finished_at">
          <dt>完成</dt>
          <dd class="tnum">{{ formatTime(task.finished_at) }}</dd>
        </template>
        <dt>链接</dt>
        <dd>
          <a class="td-link" :href="task.url" target="_blank" rel="noreferrer noopener">{{ task.url }}</a>
        </dd>
        <template v-if="task.save_path">
          <dt>保存到</dt>
          <dd class="td-path">{{ task.save_path }}</dd>
        </template>
        <template v-if="task.status === 'done'">
          <dt>AI 总结</dt>
          <dd class="td-summary-box">
            <p v-if="task.summary" class="td-summary-text">{{ task.summary }}</p>
            <RButton
              size="sm"
              variant="ghost"
              :disabled="generatingSummary"
              @click="onGenerateTaskSummary"
            >
              {{ generatingSummary ? '正在分析总结…' : task.summary ? '重新总结' : '生成 AI 总结' }}
            </RButton>
          </dd>
        </template>
      </dl>
      <slot name="overview-extra" :task="task" />
      <ul v-if="files.length" class="td-files">
        <li v-for="f in files" :key="f.id">
          <span class="td-file-name" :title="f.path">{{ fileName(f) }}</span>
          <span class="tnum td-file-size">{{ formatBytes(f.size) }}</span>
          <!-- 删文件与删任务分离：这里只删这个产物（磁盘 + 索引），任务记录保留。 -->
          <button
            type="button"
            class="td-file-del"
            :disabled="busy"
            :aria-label="`删除文件 ${fileName(f)}`"
            title="删除这个文件（不可撤销）"
            @click="requestDeleteFile(f)"
          >
            删除
          </button>
        </li>
      </ul>
    </section>

    <!-- ② 进度 -->
    <section class="td-sec">
      <h3 class="td-sec-title">进度</h3>
      <TaskProgress :task="task" />
      <dl class="td-kv">
        <dt>尝试</dt>
        <dd class="tnum">第 {{ task.attempt + 1 }}/{{ task.max_attempts }} 次</dd>
        <dt>速度</dt>
        <dd class="tnum">{{ formatSpeed(task.speed_bps) }}</dd>
        <dt>大小</dt>
        <dd class="tnum">{{ sizeText }}</dd>
      </dl>
    </section>

    <!-- ③ 日志 -->
    <section class="td-sec">
      <h3 class="td-sec-title">日志</h3>
      <ol v-if="events.length" class="td-logs">
        <li v-for="e in events" :key="e.id" class="td-log" :data-level="e.level">
          <span class="td-log-ts tnum">{{ formatTime(e.ts) }}</span>
          <span class="td-log-msg">{{ e.message }}</span>
        </li>
      </ol>
      <div v-else class="td-logs-empty">
        <RaccoonFace face="waiting" :size="28" />
        <span>还没有日志记录。任务开始动作后，这里会逐条出现。</span>
      </div>
      <slot name="logs-extra" :task="task" />
    </section>

    <!-- ④ 错误与建议（五区结构固定：没有错误时也占位，只讲一句「没有错误」） -->
    <section class="td-sec td-sec--error">
      <h3 class="td-sec-title">错误与建议</h3>
      <template v-if="showError">
        <!-- 表情按状态取用（方案 §5）：取消 → cancelled（平静省略号嘴），
             重试等待期 → waiting（眼罩半搭，还没终态化，不摆失败脸），
             失败 → failed（眼罩歪斜）。取消不是「出错」，表情不能带指责味。 -->
        <div class="td-conclusion-row">
          <RaccoonFace
            :face="task.status === 'cancelled' ? 'cancelled' : isAutoRetry ? 'waiting' : 'failed'"
            :size="36"
          />
          <p v-if="conclusion" class="td-conclusion" :class="{ 'is-retrying': isAutoRetry }">{{ conclusion }}</p>
        </div>
        <dl class="td-kv">
          <dt>错误码</dt>
          <dd class="tnum">{{ task.error_code || '—' }}</dd>
          <dt>原因</dt>
          <dd>{{ errorReason }}</dd>
          <!-- 引擎原文：分类文案说的是「哪类问题」，原文说的是「到底报了什么」。 -->
          <template v-if="engineDetail">
            <dt>引擎原文</dt>
            <dd class="td-engine-detail mono">{{ engineDetail }}</dd>
          </template>
          <dt>下一步</dt>
          <dd>{{ suggestion }}</dd>
        </dl>
        <div class="td-fix">
          <!-- Cookie / 登录失效：一键跳「连接」页（路由由上层处理，组件不写死跳转）。 -->
          <RButton v-if="needsCredentials" variant="accent" size="sm" @click="emit('goto-connections')">
            去「连接」页更新凭据
          </RButton>
          <RButton v-if="needsSettings" size="sm" @click="emit('goto-settings')">
            去「设置」页检查引擎
          </RButton>
          <RButton v-if="canRetry" size="sm" :disabled="busy" @click="emit('retry', task)">
            {{ retryLabel }}
          </RButton>
          <!-- 换通道：本地网络路径连不上时，改走 115 云端离线（后端未配置会如实 409）。 -->
          <RButton
            v-if="canReroute115"
            size="sm"
            variant="accent"
            :disabled="busy"
            title="改走 115 离线：由 115 云端抓取，绕开本机网络路径"
            @click="emit('retry-via', task, 'n115')"
          >
            转 115 通道重试
          </RButton>
          <RButton
            v-if="canRetry"
            size="sm"
            variant="ghost"
            :disabled="busy"
            title="切换使用网络代理重试（直连超时或受限时使用）"
            @click="emit('retry-proxy', task)"
          >
            切换代理重试
          </RButton>
        </div>
      </template>
      <p v-else class="td-noerr">本次没有错误记录。</p>
    </section>

    <!-- ⑤ 操作 -->
    <section class="td-sec">
      <h3 class="td-sec-title">操作</h3>
      <div class="td-ops">
        <slot name="actions" :task="task">
          <RButton v-if="isActive" size="sm" variant="ghost" :disabled="busy" @click="emit('cancel', task)">
            取消
          </RButton>
          <RButton v-if="canRetry" size="sm" :disabled="busy" @click="emit('retry', task)">重试</RButton>
          <RButton
            v-if="canRetry"
            size="sm"
            variant="ghost"
            :disabled="busy"
            title="切换使用网络代理重试"
            @click="emit('retry-proxy', task)"
          >
            切换代理重试
          </RButton>
          <RButton
            size="sm"
            variant="ghost"
            tone="danger"
            confirm="确认删除？"
            :disabled="busy"
            @click="emit('remove', task)"
          >
            删除
          </RButton>
        </slot>
      </div>
      <!-- R10：把「没有暂停按钮」这件事说清楚。
           引擎（yt-dlp / gallery-dl）本身不支持断点暂停，界面上缺一个暂停按钮
           会被当成功能缺失；写一句为什么 + 替代路径，比让用户反复找要好。 -->
      <p v-if="isActive" class="td-note">
        当前引擎不支持暂停/续传；需要中断请取消，重新入队会保留原配置版本。
      </p>
    </section>

  </div>
</template>

<style scoped>
.tdetail {
  display: flex;
  flex-direction: column;
}

/* 区块之间用细分割线分区（对齐 base/DetailDrawer 的分隔风格）。 */
.td-sec {
  padding: 12px 0;
}
.td-sec + .td-sec {
  border-top: 1px solid var(--border);
}
.td-sec:first-child {
  padding-top: 0;
}
.td-sec-title {
  margin: 0 0 8px;
  font-size: var(--font-aux);
  font-weight: var(--weight-semibold);
  color: var(--text-2);
}
.td-title {
  margin: 0 0 8px;
  font-size: var(--font-body);
  font-weight: var(--weight-medium);
  color: var(--text-1);
  word-break: break-all;
}

.td-kv {
  display: grid;
  grid-template-columns: 76px 1fr;
  gap: 6px 12px;
  margin: 0;
  font-size: var(--font-aux);
}
.td-kv dt {
  color: var(--text-3);
}
.td-kv dd {
  margin: 0;
  min-width: 0;
  word-break: break-all;
}
.td-link {
  color: var(--info);
}
.td-path {
  color: var(--text-2);
}
/* 媒体直链很长（带签名参数）：单行省略，悬停看全文。 */
.td-media-url {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.td-files {
  list-style: none;
  margin: 10px 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: var(--font-aux);
}
.td-files li {
  display: flex;
  justify-content: space-between;
  gap: 10px;
}
.td-file-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-1);
}
.td-file-size {
  color: var(--text-3);
  flex-shrink: 0;
}
/* 删文件按钮：默认低存在感（这是低频且危险的动作用），hover/聚焦才显红。 */
.td-file-del {
  flex-shrink: 0;
  border: none;
  background: none;
  padding: 0 2px;
  font-size: var(--font-aux);
  color: var(--text-3);
  cursor: pointer;
  border-radius: var(--radius-ctrl);
  transition: color 120ms ease-out;
}
.td-file-del:hover:not(:disabled),
.td-file-del:focus-visible {
  color: var(--danger);
  text-decoration: underline;
}
.td-file-del:disabled { cursor: not-allowed; opacity: 0.5; }

/* 日志：限高可滚动，日志再长也不把抽屉撑爆。 */
.td-logs {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 220px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
  font-size: var(--font-aux);
}
.td-log {
  display: flex;
  gap: 10px;
  align-items: baseline;
}
.td-log-ts {
  color: var(--text-3);
  flex-shrink: 0;
}
.td-log-msg {
  color: var(--text-1);
  word-break: break-word;
}
.td-log[data-level='error'] .td-log-msg {
  color: var(--danger);
}
.td-log[data-level='warn'] .td-log-msg {
  color: var(--warning);
}
.td-logs-empty {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-3);
  font-size: var(--font-aux);
}

.td-summary-box {
  display: flex;
  flex-direction: column;
  gap: 6px;
  align-items: flex-start;
  width: 100%;
}
.td-summary-text {
  font-size: var(--font-aux);
  color: var(--text-2);
  line-height: 1.6;
  white-space: pre-wrap;
  margin: 0;
  background: var(--surface-2);
  padding: 8px 12px;
  border-radius: var(--radius-ctrl);
  border: 1px solid var(--border);
  width: 100%;
  box-sizing: border-box;
}

/* 结论行：表情 + 人话结论并排，表情 36px 与结论基线对齐。 */
.td-conclusion-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.td-sec--error .td-conclusion {
  margin: 0;
  font-size: var(--font-body);
  color: var(--danger);
  line-height: 1.6;
}
/* 重试等待期还没终态化：警示色而非危险色，语义是「在等」，不是「完了」。 */
.td-sec--error .td-conclusion.is-retrying {
  color: var(--warning);
}
.td-noerr {
  margin: 0;
  font-size: var(--font-body);
  color: var(--text-3);
}
/* 引擎原文：等宽字体 + 弱化背景，和上方的人话「原因」明显区分——
   上面那句是结论，这段是原文，两者不能长得像。 */
.td-engine-detail {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: var(--font-aux);
  color: var(--text-2);
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  padding: 6px 10px;
  word-break: break-word;
  white-space: pre-wrap;
}
.td-fix {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
}

.td-ops {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
/* 操作区说明句：辅助档，压在按钮下方，不抢主操作的视觉权重。 */
.td-note {
  margin: 8px 0 0;
  font-size: var(--font-aux);
  color: var(--text-3);
  line-height: 1.6;
}
</style>
