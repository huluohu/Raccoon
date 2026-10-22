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
import { computed } from 'vue'
import StatusDot from './StatusDot.vue'
import TaskProgress from './TaskProgress.vue'
import PlatformBadge from './PlatformBadge.vue'
import RButton from '@/components/base/RButton.vue'
import RaccoonFace from '@/components/raccoon/RaccoonFace.vue'
import type { Task, TaskEvent, TaskFile } from '@/api/types'
import { formatBytes, formatSpeed, formatTime, WAIT_REASON_TEXT } from '@/utils/format'

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
  /** 取消任务。 */
  (e: 'cancel', task: Task): void
  /** 删除任务。 */
  (e: 'remove', task: Task): void
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

/** 错误区是否值得展示：真失败 / 有错误码 / 缺凭据。 */
const showError = computed(
  () => props.task.status === 'failed' || !!props.task.error_code || !!props.task.error_message || needsCredentials.value,
)

/** 可重试：失败或已取消的任务都能手动重来（docs/05 §7）。 */
const canRetry = computed(() => props.task.status === 'failed' || props.task.status === 'cancelled')

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
  NET_TIMEOUT: '网络超时，等一会儿重试',
  NET_RESET: '连接被中断，重试通常就能续上',
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
 * 错误结论：把「失败」翻译成一句人话。
 * 讲事实 + 小机灵鬼式幽默，但整屏只此一处拟人（docs/07 §5「每界面至多一处拟人」），其余区块保持中性。
 */
const conclusion = computed(() => {
  const code = props.task.error_code ?? ''
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
    case 'NET_RESET':
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
</script>

<template>
  <div class="tdetail">
    <!-- ① 概览 -->
    <section class="td-sec">
      <h3 class="td-sec-title">概览</h3>
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
      </dl>
      <slot name="overview-extra" :task="task" />
      <ul v-if="files.length" class="td-files">
        <li v-for="f in files" :key="f.id">
          <span class="td-file-name" :title="f.path">{{ fileName(f) }}</span>
          <span class="tnum td-file-size">{{ formatBytes(f.size) }}</span>
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
        <p v-if="conclusion" class="td-conclusion">{{ conclusion }}</p>
        <dl class="td-kv">
          <dt>错误码</dt>
          <dd class="tnum">{{ task.error_code || '—' }}</dd>
          <dt>原因</dt>
          <dd>{{ errorReason }}</dd>
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
            重试这个任务
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
            size="sm"
            variant="ghost"
            tone="danger"
            confirm="再点一次确认删除"
            :disabled="busy"
            @click="emit('remove', task)"
          >
            删除
          </RButton>
        </slot>
      </div>
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
  font-weight: 600;
  color: var(--text-2);
}
.td-title {
  margin: 0 0 8px;
  font-size: var(--font-body);
  font-weight: 650;
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

.td-files {
  list-style: none;
  margin: 10px 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: var(--font-caption);
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
  font-size: var(--font-caption);
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
  font-size: var(--font-caption);
}

.td-sec--error .td-conclusion {
  margin: 0 0 8px;
  font-size: var(--font-aux);
  color: var(--danger);
}
.td-noerr {
  margin: 0;
  font-size: var(--font-caption);
  color: var(--text-3);
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
</style>
