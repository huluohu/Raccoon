<script setup lang="ts">
// 直播页（docs/07 §3 Live / docs/04 §2.7）：
//
// 铁律：支持平台清单由后端下发（实测清单），前端不写死。
// 不在清单的平台在下拉里禁用并说明原因，绝不让用户填完才报 422。
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import { useToastStore } from '@/stores/toast'
import { formatBytes, formatDuration, formatRelative } from '@/utils/format'
import type { LiveRecording, LiveSource } from '@/api/types'
import RButton from '@/components/base/RButton.vue'
import REmpty from '@/components/base/REmpty.vue'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import PlatformBadge from '@/components/task/PlatformBadge.vue'

const toast = useToastStore()

const items = ref<LiveSource[]>([])
const supported = ref<string[]>([])
const supportedNote = ref('')
const loading = ref(true)
const loadErr = ref('')
const recordingId = ref('')

/** 新增源表单。 */
const formOpen = ref(false)
const saving = ref(false)
const form = ref({
  platform: 'bilibili',
  room_url: '',
  title: '',
  quality: 'best',
  record_plan: 'manual',
  segment_min: 30,
  check_interval_sec: 120,
  credential_id: '',
})

/** 录制历史抽屉。 */
const historyId = ref('')
const history = ref<LiveRecording[]>([])
const historyLoading = ref(false)
const historyTotal = ref({ segments: 0, size: 0 })

const PLATFORM_LABEL: Record<string, string> = {
  bilibili: '哔哩哔哩',
  douyu: '斗鱼',
  huya: '虎牙',
  kuaishou: '快手',
}
const PLANS = [
  { value: 'manual', label: '仅手动', hint: '只在点「开始录制」时录' },
  { value: 'always', label: '开播即录', hint: '检测到开播就自动录制' },
  { value: 'scheduled', label: '定时录制', hint: '按 schedule_json 设定的时段录' },
]
const STATUS_TEXT: Record<string, string> = {
  offline: '未开播',
  live: '直播中',
  recording: '录制中',
  error: '异常',
}
const STATUS_TONE: Record<string, string> = {
  offline: 'var(--neutral)',
  live: 'var(--success)',
  recording: 'var(--danger)',
  error: 'var(--warning)',
}
const REC_STATUS: Record<string, string> = {
  recording: '录制中',
  completed: '已完成',
  failed: '失败',
}

const detail = computed(() => items.value.find((s) => s.id === historyId.value) ?? null)
const liveCount = computed(() => items.value.filter((s) => s.status === 'recording' || s.status === 'live').length)

/** 不在实测清单的平台（下拉禁用并标原因）。 */
const allPlatforms = ['bilibili', 'douyu', 'huya', 'kuaishou']

function platformDisabled(p: string): boolean {
  return !supported.value.includes(p)
}

async function load() {
  loading.value = true
  loadErr.value = ''
  try {
    const res = await api.listLiveSources()
    items.value = res.items ?? []
    supported.value = res.supported ?? []
    supportedNote.value = res.supported_note ?? ''
    if (!supported.value.includes(form.value.platform)) form.value.platform = supported.value[0] ?? 'bilibili'
  } catch (e) {
    loadErr.value = e instanceof Error ? e.message : '直播源加载失败'
  } finally {
    loading.value = false
  }
}

function resetForm() {
  form.value = {
    platform: supported.value[0] ?? 'bilibili',
    room_url: '',
    title: '',
    quality: 'best',
    record_plan: 'manual',
    segment_min: 30,
    check_interval_sec: 120,
    credential_id: '',
  }
  formOpen.value = true
}

async function save() {
  saving.value = true
  try {
    await api.createLiveSource({
      platform: form.value.platform,
      room_url: form.value.room_url.trim(),
      title: form.value.title,
      quality: form.value.quality,
      record_plan: form.value.record_plan,
      segment_min: form.value.segment_min,
      check_interval_sec: form.value.check_interval_sec,
      credential_id: form.value.credential_id,
    })
    toast.success('直播源已添加')
    formOpen.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '添加失败')
  } finally {
    saving.value = false
  }
}

async function startRecord(s: LiveSource) {
  recordingId.value = s.id
  try {
    const res = await api.recordLive(s.id)
    toast.success('录制任务已入队：' + res.task.id.slice(0, 8))
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '开始录制失败')
  } finally {
    recordingId.value = ''
  }
}

async function toggleEnabled(s: LiveSource) {
  try {
    await api.updateLiveSource(s.id, { enabled: !s.enabled })
    toast.success(s.enabled ? '已停止监控 ' + (s.title || s.room_id) : '已恢复监控 ' + (s.title || s.room_id))
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '操作失败')
  }
}

async function remove(s: LiveSource) {
  try {
    await api.deleteLiveSource(s.id)
    toast.success('直播源已删除，录制文件保留')
    if (historyId.value === s.id) historyId.value = ''
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '删除失败')
  }
}

async function openHistory(s: LiveSource) {
  historyId.value = s.id
  historyLoading.value = true
  try {
    const res = await api.listLiveRecordings(s.id)
    history.value = res.items ?? []
    historyTotal.value = history.value.reduce(
      (acc, r) => ({ segments: acc.segments + (r.segments ?? 0), size: acc.size + (r.size ?? 0) }),
      { segments: 0, size: 0 },
    )
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '录制历史加载失败')
  } finally {
    historyLoading.value = false
  }
}

function lastCheckText(s: LiveSource): string {
  if (!s.last_checked_at) return '未检测过'
  return formatRelative(s.last_checked_at) + '检测'
}

onMounted(load)
</script>

<template>
  <section class="page">
    <header class="page-head">
      <div>
        <h1 class="page-title">直播</h1>
        <p class="page-sub">
          直播源 {{ items.length }} 个，直播中 {{ liveCount }} 个 · {{ supportedNote }}
        </p>
      </div>
      <RButton variant="accent" :disabled="supported.length === 0" @click="resetForm">添加直播源</RButton>
    </header>

    <p class="platform-note">
      实测支持：
      <span v-for="p in supported" :key="p" class="chip">{{ PLATFORM_LABEL[p] || p }}</span>
      <span v-if="supported.length === 0" class="chip chip--off">暂无</span>
      其他平台不开放添加——开播检测没实测过，录出来大概率是空文件。
    </p>

    <p v-if="loadErr" class="alert alert--danger">{{ loadErr }}</p>

    <REmpty
      v-else-if="!loading && items.length === 0"
      face="waiting"
      title="还没有直播源"
      hint="贴直播间地址进来，能开播检测 + 一键录制，录制任务和普通下载走同一条队列。"
      action-label="添加第一个直播源"
      @action="resetForm"
    />

    <div v-else class="live-list">
      <article v-for="s in items" :key="s.id" class="card" :class="{ off: !s.enabled }">
        <header class="card-head">
          <PlatformBadge :platform="s.platform" />
          <span class="status" :style="{ color: STATUS_TONE[s.status] }">
            <span class="dot" :style="{ background: STATUS_TONE[s.status] }" aria-hidden="true" />
            {{ STATUS_TEXT[s.status] || s.status }}
          </span>
          <span class="plan">{{ PLANS.find((p) => p.value === s.record_plan)?.label || s.record_plan }}</span>
        </header>

        <h3 class="card-title">{{ s.title || s.room_id }}</h3>
        <p class="card-url mono" :title="s.room_url">{{ s.room_url }}</p>

        <dl class="meta">
          <div><dt>房间号</dt><dd>{{ s.room_id }}</dd></div>
          <div><dt>画质</dt><dd>{{ s.quality || 'best' }}</dd></div>
          <div><dt>分段</dt><dd>{{ s.segment_min || 0 }} 分钟</dd></div>
          <div><dt>检测</dt><dd>{{ lastCheckText(s) }} · 每 {{ s.check_interval_sec || 0 }}s</dd></div>
          <div><dt>录制次数</dt><dd>{{ s.recording_count ?? 0 }}</dd></div>
          <div v-if="s.last_live_at"><dt>上次开播</dt><dd>{{ formatRelative(s.last_live_at) }}</dd></div>
        </dl>

        <p v-if="s.error_note" class="err-note">{{ s.error_note }}</p>

        <footer class="card-foot">
          <RButton
            size="sm"
            variant="accent"
            :disabled="recordingId === s.id || s.status === 'recording'"
            @click="startRecord(s)"
          >
            {{ s.status === 'recording' ? '录制中' : recordingId === s.id ? '提交中…' : '开始录制' }}
          </RButton>
          <RButton size="sm" variant="ghost" @click="openHistory(s)">录制历史</RButton>
          <RButton size="sm" variant="ghost" @click="toggleEnabled(s)">{{ s.enabled ? '停止监控' : '恢复监控' }}</RButton>
          <RButton size="sm" variant="ghost" tone="danger" confirm="再点一次确认删除" @click="remove(s)">删除</RButton>
        </footer>
      </article>
    </div>

    <!-- 添加直播源 -->
    <DetailDrawer :open="formOpen" title="添加直播源" @close="formOpen = false">
      <form class="form" @submit.prevent="save">
        <label class="field">
          <span class="field-label">平台</span>
          <select v-model="form.platform">
            <option v-for="p in allPlatforms" :key="p" :value="p" :disabled="platformDisabled(p)">
              {{ PLATFORM_LABEL[p] || p }}{{ platformDisabled(p) ? '（未实测，不支持）' : '' }}
            </option>
          </select>
          <span class="field-hint">{{ supportedNote }}</span>
        </label>

        <label class="field">
          <span class="field-label">直播间地址</span>
          <input
            v-model="form.room_url"
            type="text"
            spellcheck="false"
            placeholder="https://live.bilibili.com/{room}"
          />
        </label>

        <label class="field">
          <span class="field-label">备注名（可选）</span>
          <input v-model="form.title" type="text" placeholder="如：晚间场" maxlength="80" />
        </label>

        <label class="field">
          <span class="field-label">画质</span>
          <input v-model="form.quality" type="text" placeholder="best / 1080p / 720p" />
        </label>

        <label class="field">
          <span class="field-label">录制计划</span>
          <select v-model="form.record_plan">
            <option v-for="p in PLANS" :key="p.value" :value="p.value">{{ p.label }}</option>
          </select>
          <span class="field-hint">{{ PLANS.find((p) => p.value === form.record_plan)?.hint }}</span>
        </label>

        <div class="two-col">
          <label class="field">
            <span class="field-label">分段时长（分钟）</span>
            <input v-model.number="form.segment_min" type="number" min="1" max="240" />
          </label>
          <label class="field">
            <span class="field-label">开播检测间隔（秒）</span>
            <input v-model.number="form.check_interval_sec" type="number" min="30" max="3600" />
          </label>
        </div>

        <div class="form-foot">
          <RButton variant="ghost" @click="formOpen = false">取消</RButton>
          <RButton variant="accent" type="submit" :disabled="saving">{{ saving ? '保存中…' : '添加' }}</RButton>
        </div>
      </form>
    </DetailDrawer>

    <!-- 录制历史 -->
    <DetailDrawer :open="historyId !== ''" :title="(detail?.title || detail?.room_id || '') + ' · 录制历史'" @close="historyId = ''">
      <p class="hist-sum">
        共 {{ history.length }} 次录制 · {{ historyTotal.segments }} 个分段 · {{ formatBytes(historyTotal.size) }}
      </p>
      <p v-if="historyLoading" class="field-hint">加载中…</p>
      <p v-else-if="history.length === 0" class="field-hint">还没有录制记录。</p>
      <table v-else class="tbl">
        <thead>
          <tr><th>开始</th><th>时长</th><th>分段</th><th>大小</th><th>状态</th></tr>
        </thead>
        <tbody>
          <tr v-for="r in history" :key="r.id">
            <td class="tnum">{{ new Date(r.started_at).toLocaleString('zh-CN') }}</td>
            <td class="tnum">{{ r.ended_at ? formatDuration((r.ended_at - r.started_at) / 1000) : '进行中' }}</td>
            <td class="tnum">{{ r.segments }}</td>
            <td class="tnum">{{ formatBytes(r.size) }}</td>
            <td :style="{ color: r.status === 'completed' ? 'var(--success)' : r.status === 'failed' ? 'var(--danger)' : 'var(--info)' }">
              {{ REC_STATUS[r.status] || r.status }}
            </td>
          </tr>
        </tbody>
      </table>
    </DetailDrawer>
  </section>
</template>

<style scoped>
.page-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.page-title { font-size: 24px; font-weight: 750; border-left: 4px solid var(--gold); padding-left: 12px; }
.page-sub { margin-top: 6px; font-size: var(--font-aux); color: var(--text-3); }
.platform-note {
  font-size: var(--font-caption);
  color: var(--text-3);
  margin-bottom: 14px;
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.chip {
  font-size: var(--font-caption);
  padding: 1px 8px;
  border-radius: var(--radius-pill);
  background: var(--gold-soft);
  color: var(--gold-strong);
  border: 1px solid var(--gold);
}
.chip--off { background: var(--surface-2); color: var(--text-3); border-color: var(--border); }
.alert { padding: 10px 14px; margin-bottom: 14px; border-radius: var(--radius-ctrl); border: 1px solid var(--border); font-size: var(--font-aux); }
.alert--danger { border-color: var(--danger); color: var(--danger); }

.live-list { display: flex; flex-direction: column; gap: 12px; }
.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  box-shadow: var(--shadow-1);
}
.card.off { opacity: 0.66; }
.card-head { display: flex; align-items: center; gap: 10px; }
.status { display: inline-flex; align-items: center; gap: 5px; font-size: var(--font-caption); font-weight: 600; }
.dot { width: 8px; height: 8px; border-radius: 50%; }
.plan {
  margin-left: auto;
  font-size: var(--font-caption);
  color: var(--text-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 1px 8px;
}
.card-title { font-size: var(--font-title-3); font-weight: 650; }
.card-url { font-size: var(--font-caption); color: var(--text-3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.meta { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 4px 16px; }
.meta > div { display: flex; gap: 8px; font-size: var(--font-caption); }
.meta dt { color: var(--text-3); width: 56px; flex-shrink: 0; }
.meta dd { color: var(--text-2); }
.err-note { font-size: var(--font-caption); color: var(--danger); }
.card-foot { display: flex; gap: 6px; flex-wrap: wrap; padding-top: 8px; border-top: 1px solid var(--border); }

.form { display: flex; flex-direction: column; gap: 14px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field-label { font-size: var(--font-aux); font-weight: 600; color: var(--text-2); }
.field input, .field select {
  padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--radius-ctrl);
  background: var(--bg); color: var(--text-1); font-size: var(--font-aux); font-family: inherit;
  width: 100%; box-sizing: border-box;
}
.field input:focus, .field select:focus { outline: 2px solid var(--gold); outline-offset: 1px; }
.field-hint { font-size: var(--font-caption); color: var(--text-3); }
.two-col { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.form-foot { display: flex; justify-content: flex-end; gap: 8px; padding-top: 8px; border-top: 1px solid var(--border); }

.hist-sum { font-size: var(--font-aux); color: var(--text-2); margin-bottom: 10px; }
.tbl { width: 100%; border-collapse: collapse; font-size: var(--font-caption); }
.tbl th { text-align: left; color: var(--text-3); font-weight: 500; padding: 6px 8px; border-bottom: 1px solid var(--border); }
.tbl td { padding: 6px 8px; border-bottom: 1px solid var(--border); color: var(--text-2); }
.tbl .tnum { white-space: nowrap; }
</style>