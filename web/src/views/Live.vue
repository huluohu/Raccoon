<script setup lang="ts">
// 直播页（docs/07 §3 Live / docs/04 §2.7）：
//
// 铁律：支持平台清单由后端下发（实测清单），前端不写死。
// 不在清单的平台在下拉里禁用并说明原因，绝不让用户填完才报 422。
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import { useToastStore } from '@/stores/toast'
import type { LiveRecording, LiveSource } from '@/api/types'
import RButton from '@/components/base/RButton.vue'
import PageHeader from '@/components/base/PageHeader.vue'
import REmpty from '@/components/base/REmpty.vue'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import SourceList from '@/components/live/SourceList.vue'
import RecordPlanForm from '@/components/live/RecordPlanForm.vue'
import type { LiveFormValue } from '@/components/live/RecordPlanForm.vue'
import RecordingList from '@/components/live/RecordingList.vue'

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
/** 传给 RecordPlanForm 的初始值（每次打开都重置）。 */
const formInitial = ref<Record<string, unknown>>({})

/** 录制历史抽屉。 */
const historyId = ref('')
const history = ref<LiveRecording[]>([])
const historyLoading = ref(false)
const historyTotal = ref({ segments: 0, size: 0 })

/** 平台中文名（页面顶部的「实测支持」标签用）。 */
const PLATFORM_LABEL: Record<string, string> = {
  bilibili: '哔哩哔哩',
  douyu: '斗鱼',
  huya: '虎牙',
  kuaishou: '快手',
}

const detail = computed(() => items.value.find((s) => s.id === historyId.value) ?? null)
const liveCount = computed(() => items.value.filter((s) => s.status === 'recording' || s.status === 'live').length)

async function load() {
  loading.value = true
  loadErr.value = ''
  try {
    const res = await api.listLiveSources()
    items.value = res.items ?? []
    supported.value = res.supported ?? []
    supportedNote.value = res.supported_note ?? ''
    // 后端清单可能与前端候选不同：以清单第一个为准（表单打开时会带上）。
    if (supported.value.length && !supported.value.includes(formInitial.value.platform as string)) {
      formInitial.value = { platform: supported.value[0] }
    }
  } catch (e) {
    loadErr.value = e instanceof Error ? e.message : '直播源加载失败'
  } finally {
    loading.value = false
  }
}

function resetForm() {
  // 换一个新对象引用 → RecordPlanForm 的 watch 会把表单重置成默认值。
  formInitial.value = { platform: supported.value[0] ?? 'bilibili' }
  formOpen.value = true
}

async function save(value: LiveFormValue) {
  saving.value = true
  try {
    await api.createLiveSource({
      platform: value.platform,
      room_url: value.room_url.trim(),
      title: value.title,
      quality: value.quality,
      record_plan: value.record_plan,
      schedule_json: value.schedule_json,
      segment_min: value.segment_min,
      check_interval_sec: value.check_interval_sec,
      credential_id: value.credential_id,
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

onMounted(load)
</script>

<template>
  <section class="page">
    <PageHeader
      eyebrow="LIVE"
      title="直播"
      :sub="`直播源 ${items.length} 个，直播中 ${liveCount} 个 · ${supportedNote}`"
    >
      <template #actions>
        <!-- 空态时页头不放按钮：空态卡里已有「添加第一个直播源」，两个同功能
             按钮同屏是重复（2026-10-24 用户反馈）。有源后才显示页头入口。 -->
        <RButton v-if="items.length > 0" variant="accent" :disabled="supported.length === 0" @click="resetForm">添加直播源</RButton>
      </template>
    </PageHeader>

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

    <!-- 直播源列表（方案 §3 视图级组件 SourceList） -->
    <SourceList
      v-else
      :items="items"
      :recording-id="recordingId"
      @record="startRecord"
      @history="openHistory"
      @toggle="toggleEnabled"
      @remove="remove"
    />

    <!-- 添加直播源（方案 §3 视图级组件 RecordPlanForm） -->
    <DetailDrawer :open="formOpen" title="添加直播源" @close="formOpen = false">
      <RecordPlanForm
        :supported="supported"
        :supported-note="supportedNote"
        :saving="saving"
        :initial="formInitial"
        @save="save"
        @cancel="formOpen = false"
      />
    </DetailDrawer>

    <!-- 录制历史（方案 §3 视图级组件 RecordingList） -->
    <DetailDrawer
      :open="historyId !== ''"
      :title="(detail?.title || detail?.room_id || '') + ' · 录制历史'"
      @close="historyId = ''"
    >
      <p v-if="historyLoading" class="field-hint">加载中…</p>
      <RecordingList
        v-else
        :recordings="history"
        :total-segments="historyTotal.segments"
        :total-size="historyTotal.size"
      />
    </DetailDrawer>
  </section>
</template>

<style scoped>
/* 页面副标题是正文档（16px）。 */
.platform-note {
  font-size: var(--font-body);
  color: var(--text-3);
  margin-bottom: 16px;
  display: flex;
  align-items: center;
  gap: 8px;
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
.alert { padding: 12px 16px; margin-bottom: 16px; border-radius: var(--radius-ctrl); border: 1px solid var(--border); font-size: var(--font-body); }
.alert--danger { border-color: var(--danger); color: var(--danger); }

.meta dt { color: var(--text-3); width: 60px; flex-shrink: 0; }
.meta dd { color: var(--text-2); }

/* 表单字段名：正文档 16px。 */

/* 表格：表头 caption(12px)，表体 body(16px)。 */
</style>