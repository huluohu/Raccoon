<script setup lang="ts">
// 订阅页（docs/07 §3 Subscriptions / docs/04 §2.6）：
//
// 交互要点：新建前先「解析预览」——真调 yt-dlp 拉来源元信息，解析不到内容直接报错，
// 绝不静默建一个空订阅。检查走后端队列（check-now 同步等结果，最长 90s）。
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import { useToastStore } from '@/stores/toast'
import { formatRelative } from '@/utils/format'
import type { ParsePreview, Subscription, SubscriptionItem } from '@/api/types'
import RButton from '@/components/base/RButton.vue'
import REmpty from '@/components/base/REmpty.vue'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import PlatformBadge from '@/components/task/PlatformBadge.vue'

const toast = useToastStore()

const items = ref<Subscription[]>([])
const loading = ref(true)
const loadErr = ref('')
const checkingId = ref('')

/** 新建表单。 */
const formOpen = ref(false)
const saving = ref(false)
const previewing = ref(false)
const preview = ref<ParsePreview | null>(null)
const form = ref({
  source_url: '',
  source_type: '',
  mode: 'new_only',
  quality: '',
  save_rule: '',
  interval_min: 30,
  range_from: 1,
  range_to: 20,
  credential_id: '',
})

/** 详情抽屉：某订阅的已发现内容。 */
const detailId = ref('')
const detailItems = ref<SubscriptionItem[]>([])
const detailLoading = ref(false)

const SOURCE_TYPES = [
  { value: '', label: '自动识别' },
  { value: 'favorite', label: '收藏夹' },
  { value: 'uploader', label: 'UP 主投稿' },
  { value: 'list', label: '播放列表' },
]

const MODES = [
  { value: 'new_only', label: '只要新增', hint: '已下过的条目自动跳过，适合长期追更' },
  { value: 'backfill', label: '全部补齐', hint: '把来源里没下过的内容都补下' },
  { value: 'range', label: '指定区间', hint: '只抓第 N 到第 M 条（1 起，含两端）' },
]

const MODE_TEXT: Record<string, string> = {
  new_only: '只要新增',
  backfill: '全部补齐',
  range: '指定区间',
}
const ITEM_STATUS: Record<string, string> = {
  pending: '待入队',
  queued: '已入队',
  downloaded: '已下载',
  skipped: '已跳过',
  failed: '失败',
}
const ITEM_TONE: Record<string, string> = {
  pending: 'var(--neutral)',
  queued: 'var(--info)',
  downloaded: 'var(--success)',
  skipped: 'var(--text-3)',
  failed: 'var(--danger)',
}

const detail = computed(() => items.value.find((s) => s.id === detailId.value) ?? null)
const rangeVisible = computed(() => form.value.mode === 'range')
const activeCount = computed(() => items.value.filter((s) => s.enabled).length)
const totalQueued = computed(() =>
  items.value.reduce((sum, s) => sum + (s.item_counts?.queued ?? 0), 0),
)

async function load() {
  loading.value = true
  loadErr.value = ''
  try {
    const res = await api.listSubscriptions()
    items.value = res.items ?? []
  } catch (e) {
    loadErr.value = e instanceof Error ? e.message : '订阅加载失败'
  } finally {
    loading.value = false
  }
}

function resetForm() {
  preview.value = null
  form.value = {
    source_url: '',
    source_type: '',
    mode: 'new_only',
    quality: '',
    save_rule: '',
    interval_min: 30,
    range_from: 1,
    range_to: 20,
    credential_id: '',
  }
  formOpen.value = true
}

async function doPreview() {
  const url = form.value.source_url.trim()
  if (!url) {
    toast.error('先填来源链接')
    return
  }
  previewing.value = true
  preview.value = null
  try {
    const res = await api.createSubscription({ source_url: url, preview: true })
    preview.value = res.data as ParsePreview
    toast.success('来源解析成功，确认无误再保存')
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '来源解析失败')
  } finally {
    previewing.value = false
  }
}

async function save() {
  saving.value = true
  try {
    const body: Record<string, unknown> = {
      source_url: form.value.source_url.trim(),
      mode: form.value.mode,
      interval_min: form.value.interval_min,
    }
    if (form.value.source_type) body.source_type = form.value.source_type
    if (form.value.quality) body.quality = form.value.quality
    if (form.value.save_rule) body.save_rule = form.value.save_rule
    if (form.value.credential_id) body.credential_id = form.value.credential_id
    if (form.value.mode === 'range') {
      body.range_from = form.value.range_from
      body.range_to = form.value.range_to
    }
    await api.createSubscription(body as never)
    toast.success('订阅已创建，后台每 ' + (form.value.interval_min || 30) + ' 分钟检查一次')
    formOpen.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '订阅创建失败')
  } finally {
    saving.value = false
  }
}

async function checkNow(s: Subscription) {
  checkingId.value = s.id
  try {
    const res = await api.checkSubscriptionNow(s.id)
    if (res.checking) {
      toast.info('该订阅正在检查中，本次请求跳过')
    } else {
      const n = res.queued ?? 0
      if (n > 0) toast.success('发现 ' + n + ' 条新内容，已入队下载')
      else toast.info('没有新内容')
    }
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '检查失败')
  } finally {
    checkingId.value = ''
  }
}

async function toggleEnabled(s: Subscription) {
  try {
    await api.updateSubscription(s.id, { enabled: !s.enabled })
    toast.success((s.enabled ? '已暂停 ' : '已启用 ') + (s.title || s.id))
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '操作失败')
  }
}

async function remove(s: Subscription) {
  try {
    await api.deleteSubscription(s.id)
    toast.success('订阅已删除，已下载文件保留')
    if (detailId.value === s.id) detailId.value = ''
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '删除失败')
  }
}

async function openDetail(s: Subscription) {
  detailId.value = s.id
  detailLoading.value = true
  try {
    const res = await api.listSubscriptionItems(s.id)
    detailItems.value = res.items ?? []
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '内容列表加载失败')
  } finally {
    detailLoading.value = false
  }
}

function nextCheckText(s: Subscription): string {
  if (!s.enabled) return '已暂停'
  if (!s.next_check_at) return '待调度'
  const left = s.next_check_at - Date.now()
  if (left <= 0) return '即将检查'
  return Math.ceil(left / 60000) + ' 分钟后检查'
}

onMounted(load)
</script>

<template>
  <section class="page">
    <header class="page-head">
      <div>
        <h1 class="page-title">订阅</h1>
        <p class="page-sub">
          收藏夹 / UP 主 / 播放列表定时检查 · 启用 {{ activeCount }} 个，队列待下 {{ totalQueued }} 条
        </p>
      </div>
      <RButton variant="accent" @click="resetForm">新建订阅</RButton>
    </header>

    <p v-if="loadErr" class="alert alert--danger">{{ loadErr }}</p>

    <REmpty
      v-else-if="!loading && items.length === 0"
      face="waiting"
      title="还没有订阅"
      hint="把 B 站收藏夹或 UP 主投稿页贴进来，新内容上线自动进队列，不用天天盯。"
      action-label="新建第一个订阅"
      @action="resetForm"
    />

    <div v-else class="sub-list">
      <article v-for="s in items" :key="s.id" class="card" :class="{ off: !s.enabled }">
        <header class="card-head">
          <PlatformBadge :platform="s.platform" />
          <span class="src-type">{{ SOURCE_TYPES.find((t) => t.value === s.source_type)?.label || s.source_type }}</span>
          <span class="mode">{{ MODE_TEXT[s.mode] || s.mode }}</span>
          <span v-if="s.error_note" class="warn-mark" :title="s.error_note">!</span>
        </header>

        <h3 class="card-title">{{ s.title || s.source_url }}</h3>
        <p class="card-url mono" :title="s.source_url">{{ s.source_url }}</p>

        <div class="counts">
          <span class="cnt done">{{ s.item_counts?.downloaded ?? 0 }} 已下载</span>
          <span class="cnt queued">{{ s.item_counts?.queued ?? 0 }} 待下载</span>
          <span v-if="(s.item_counts?.failed ?? 0) > 0" class="cnt failed">{{ s.item_counts?.failed }} 失败</span>
          <span v-if="(s.item_counts?.skipped ?? 0) > 0" class="cnt skip">{{ s.item_counts?.skipped }} 已跳过</span>
        </div>

        <dl class="meta">
          <div><dt>上次检查</dt><dd>{{ s.last_checked_at ? formatRelative(s.last_checked_at) : '从未' }}</dd></div>
          <div><dt>下次</dt><dd>{{ nextCheckText(s) }}</dd></div>
          <div><dt>画质</dt><dd>{{ s.quality || '跟随全局' }}</dd></div>
          <div><dt>周期</dt><dd>{{ s.interval_min }} 分钟</dd></div>
        </dl>

        <p v-if="s.error_note" class="err-note">{{ s.error_note }}</p>

        <footer class="card-foot">
          <RButton size="sm" variant="accent" :disabled="checkingId === s.id" @click="checkNow(s)">
            {{ checkingId === s.id ? '检查中…' : '立即检查' }}
          </RButton>
          <RButton size="sm" variant="ghost" @click="openDetail(s)">已发现内容</RButton>
          <RButton size="sm" variant="ghost" @click="toggleEnabled(s)">{{ s.enabled ? '暂停' : '启用' }}</RButton>
          <RButton size="sm" variant="ghost" tone="danger" confirm="再点一次确认删除" @click="remove(s)">删除</RButton>
        </footer>
      </article>
    </div>

    <!-- 新建订阅 -->
    <DetailDrawer :open="formOpen" title="新建订阅" @close="formOpen = false">
      <form class="form" @submit.prevent="save">
        <label class="field">
          <span class="field-label">来源链接</span>
          <textarea
            v-model="form.source_url"
            rows="3"
            spellcheck="false"
            placeholder="https://space.bilibili.com/{mid}/favlist?fid=xxx 或 UP 主投稿页"
          />
        </label>

        <div class="preview-row">
          <RButton size="sm" :disabled="previewing" @click="doPreview">
            {{ previewing ? '解析中…' : '解析预览' }}
          </RButton>
          <span class="field-hint">先解析确认来源能读到，再保存</span>
        </div>

        <div v-if="preview" class="preview-box">
          <p class="pv-title">{{ preview.title || '（无标题）' }}</p>
          <dl class="pv-meta">
            <div><dt>平台</dt><dd>{{ preview.platform }}</dd></div>
            <div>
              <dt>类型</dt>
              <dd>{{ preview.is_playlist ? '集合（' + (preview.formats?.length ?? 0) + ' 项）' : '单视频' }}</dd>
            </div>
            <div v-if="preview.uploader"><dt>作者</dt><dd>{{ preview.uploader }}</dd></div>
          </dl>
        </div>

        <label class="field">
          <span class="field-label">来源类型</span>
          <select v-model="form.source_type">
            <option v-for="t in SOURCE_TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
          </select>
        </label>

        <label class="field">
          <span class="field-label">抓取模式</span>
          <select v-model="form.mode">
            <option v-for="m in MODES" :key="m.value" :value="m.value">{{ m.label }}</option>
          </select>
          <span class="field-hint">{{ MODES.find((m) => m.value === form.mode)?.hint }}</span>
        </label>

        <div v-if="rangeVisible" class="range-row">
          <label class="field">
            <span class="field-label">起始条目</span>
            <input v-model.number="form.range_from" type="number" min="1" />
          </label>
          <label class="field">
            <span class="field-label">结束条目</span>
            <input v-model.number="form.range_to" type="number" :min="form.range_from" />
          </label>
        </div>

        <label class="field">
          <span class="field-label">画质（可选）</span>
          <input v-model="form.quality" type="text" placeholder="如 best / 1080p，留空跟随全局" />
        </label>

        <label class="field">
          <span class="field-label">检查周期（分钟）</span>
          <input v-model.number="form.interval_min" type="number" min="5" max="1440" />
        </label>

        <div class="form-foot">
          <RButton variant="ghost" @click="formOpen = false">取消</RButton>
          <RButton variant="accent" type="submit" :disabled="saving || !preview">
            {{ saving ? '保存中…' : preview ? '创建订阅' : '请先解析预览' }}
          </RButton>
        </div>
      </form>
    </DetailDrawer>

    <!-- 已发现内容 -->
    <DetailDrawer :open="detailId !== ''" :title="detail?.title || '已发现内容'" @close="detailId = ''">
      <p v-if="detailLoading" class="field-hint">加载中…</p>
      <p v-else-if="detailItems.length === 0" class="field-hint">
        还没有发现内容。点「立即检查」拉一次来源。
      </p>
      <table v-else class="tbl">
        <thead>
          <tr><th>标题</th><th>状态</th><th>发现时间</th></tr>
        </thead>
        <tbody>
          <tr v-for="it in detailItems" :key="it.id">
            <td class="ttl">{{ it.title || it.item_id }}</td>
            <td><span class="st" :style="{ color: ITEM_TONE[it.status] }">{{ ITEM_STATUS[it.status] || it.status }}</span></td>
            <td class="tnum">{{ formatRelative(it.discovered_at) }}</td>
          </tr>
        </tbody>
      </table>
    </DetailDrawer>
  </section>
</template>

<style scoped>
.page-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
.page-title { font-size: 24px; font-weight: 750; border-left: 4px solid var(--gold); padding-left: 12px; }
.page-sub { margin-top: 6px; font-size: var(--font-aux); color: var(--text-3); }
.alert { padding: 10px 14px; margin-bottom: 14px; border-radius: var(--radius-ctrl); border: 1px solid var(--border); font-size: var(--font-aux); }
.alert--danger { border-color: var(--danger); color: var(--danger); }

.sub-list { display: flex; flex-direction: column; gap: 12px; }
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
.card-head { display: flex; align-items: center; gap: 8px; }
.src-type, .mode {
  font-size: var(--font-caption);
  color: var(--text-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 1px 8px;
}
.warn-mark {
  margin-left: auto;
  width: 18px; height: 18px;
  border-radius: 50%;
  background: var(--gold-soft);
  color: var(--gold-strong);
  font-weight: 700;
  font-size: 12px;
  display: inline-flex; align-items: center; justify-content: center;
  border: 1px solid var(--gold);
}
.card-title { font-size: var(--font-title-3); font-weight: 650; }
.card-url { font-size: var(--font-caption); color: var(--text-3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.counts { display: flex; gap: 8px; flex-wrap: wrap; }
.cnt { font-size: var(--font-caption); padding: 1px 8px; border-radius: var(--radius-pill); background: var(--surface-2); color: var(--text-2); }
.cnt.done { color: var(--success); }
.cnt.queued { color: var(--info); }
.cnt.failed { color: var(--danger); }
.cnt.skip { color: var(--text-3); }
.meta { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 4px 16px; }
.meta > div { display: flex; gap: 8px; font-size: var(--font-caption); }
.meta dt { color: var(--text-3); width: 56px; flex-shrink: 0; }
.meta dd { color: var(--text-2); }
.err-note { font-size: var(--font-caption); color: var(--danger); }
.card-foot { display: flex; gap: 6px; flex-wrap: wrap; padding-top: 8px; border-top: 1px solid var(--border); }

.form { display: flex; flex-direction: column; gap: 14px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field-label { font-size: var(--font-aux); font-weight: 600; color: var(--text-2); }
.field input, .field select, .field textarea {
  padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--radius-ctrl);
  background: var(--bg); color: var(--text-1); font-size: var(--font-aux); font-family: inherit;
  width: 100%; box-sizing: border-box;
}
.field textarea { resize: vertical; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.field input:focus, .field select:focus, .field textarea:focus { outline: 2px solid var(--gold); outline-offset: 1px; }
.field-hint { font-size: var(--font-caption); color: var(--text-3); }
.preview-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.preview-box { padding: 10px 12px; border-radius: var(--radius-ctrl); background: var(--surface-2); border: 1px solid var(--border); }
.pv-title { font-size: var(--font-aux); font-weight: 650; margin-bottom: 6px; }
.pv-meta { display: flex; flex-direction: column; gap: 3px; }
.pv-meta > div { display: flex; gap: 8px; font-size: var(--font-caption); }
.pv-meta dt { color: var(--text-3); width: 40px; }
.range-row { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.form-foot { display: flex; justify-content: flex-end; gap: 8px; padding-top: 8px; border-top: 1px solid var(--border); }

.tbl { width: 100%; border-collapse: collapse; font-size: var(--font-caption); }
.tbl th { text-align: left; color: var(--text-3); font-weight: 500; padding: 6px 8px; border-bottom: 1px solid var(--border); }
.tbl td { padding: 6px 8px; border-bottom: 1px solid var(--border); color: var(--text-2); }
.tbl .ttl { max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-1); }
.tbl .tnum { white-space: nowrap; }
.st { font-weight: 600; }
</style>