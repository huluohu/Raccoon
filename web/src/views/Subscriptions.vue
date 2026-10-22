<script setup lang="ts">
// 订阅页（docs/07 §3 Subscriptions / docs/04 §2.6）：
//
// 交互要点：新建前先「解析预览」——真调 yt-dlp 拉来源元信息，解析不到内容直接报错，
// 绝不静默建一个空订阅。检查走后端队列（check-now 同步等结果，最长 90s）。
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import { useToastStore } from '@/stores/toast'
import { errorText } from '@/utils/format'
import type { ParsePreview, Subscription, SubscriptionItem } from '@/api/types'
import RButton from '@/components/base/RButton.vue'
import REmpty from '@/components/base/REmpty.vue'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import SubList from '@/components/subs/SubList.vue'
import SubForm from '@/components/subs/SubForm.vue'
import type { SubFormValue } from '@/components/subs/SubForm.vue'
import ItemsTable from '@/components/subs/ItemsTable.vue'

const toast = useToastStore()

const items = ref<Subscription[]>([])
const loading = ref(true)
const loadErr = ref('')
const checkingId = ref('')

/** 新建表单：抽屉开关 + 预览状态。表单值由 SubForm 组件内部持有（§3 SubForm）。 */
const formOpen = ref(false)
const saving = ref(false)
const previewing = ref(false)
const preview = ref<ParsePreview | null>(null)
/** 传给 SubForm 的初始值（每次打开都重置成默认）。 */
const formInitial = ref<Record<string, unknown>>({})

/** 详情抽屉：某订阅的已发现内容。 */
const detailId = ref('')
const detailItems = ref<SubscriptionItem[]>([])
const detailLoading = ref(false)

const detail = computed(() => items.value.find((s) => s.id === detailId.value) ?? null)
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
    loadErr.value = errorText(e, '订阅加载失败')
  } finally {
    loading.value = false
  }
}

function resetForm() {
  preview.value = null
  // 换一个新对象引用 → SubForm 的 watch 会把表单重置成默认值。
  formInitial.value = {}
  formOpen.value = true
}

/** SubForm 组件实例：解析预览需要读它内部的 source_url。 */
const subFormRef = ref<InstanceType<typeof SubForm> | null>(null)

async function doPreview() {
  const url = (subFormRef.value?.sourceUrl ?? '').trim()
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
    toast.error(errorText(e, '来源解析失败'))
  } finally {
    previewing.value = false
  }
}

async function save(value: SubFormValue) {
  saving.value = true
  try {
    const body: Record<string, unknown> = {
      source_url: value.source_url.trim(),
      mode: value.mode,
      interval_min: value.interval_min,
    }
    if (value.source_type) body.source_type = value.source_type
    if (value.quality) body.quality = value.quality
    if (value.save_rule) body.save_rule = value.save_rule
    if (value.credential_id) body.credential_id = value.credential_id
    if (value.mode === 'range') {
      body.range_from = value.range_from
      body.range_to = value.range_to
    }
    await api.createSubscription(body as never)
    toast.success('订阅已创建，后台每 ' + (value.interval_min || 30) + ' 分钟检查一次')
    formOpen.value = false
    await load()
  } catch (e) {
    toast.error(errorText(e, '订阅创建失败'))
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
    toast.error(errorText(e, '检查失败'))
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
    toast.error(errorText(e, '操作失败'))
  }
}

async function remove(s: Subscription) {
  try {
    await api.deleteSubscription(s.id)
    toast.success('订阅已删除，已下载文件保留')
    if (detailId.value === s.id) detailId.value = ''
    await load()
  } catch (e) {
    toast.error(errorText(e, '删除失败'))
  }
}

async function openDetail(s: Subscription) {
  detailId.value = s.id
  detailLoading.value = true
  try {
    const res = await api.listSubscriptionItems(s.id)
    detailItems.value = res.items ?? []
  } catch (e) {
    toast.error(errorText(e, '内容列表加载失败'))
  } finally {
    detailLoading.value = false
  }
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

    <!-- 订阅列表（方案 §3 视图级组件 SubList） -->
    <SubList
      v-else
      :items="items"
      :checking-id="checkingId"
      @check="checkNow"
      @detail="openDetail"
      @toggle="toggleEnabled"
      @remove="remove"
    />

    <!-- 新建订阅（方案 §3 视图级组件 SubForm：先预览后创建） -->
    <DetailDrawer :open="formOpen" title="新建订阅" @close="formOpen = false">
      <SubForm
        ref="subFormRef"
        :preview="preview"
        :previewing="previewing"
        :saving="saving"
        :initial="formInitial"
        @preview="doPreview"
        @save="save"
        @cancel="formOpen = false"
      />
    </DetailDrawer>

    <!-- 已发现内容 -->
    <DetailDrawer :open="detailId !== ''" :title="detail?.title || '已发现内容'" @close="detailId = ''">
      <p v-if="detailLoading" class="field-hint">加载中…</p>
      <REmpty
        v-else-if="detailItems.length === 0"
        face="waiting"
        title="还没发现内容"
        hint="点「立即检查」拉一次来源，有新片就能追。"
      />
      <!-- 已发现内容（方案 §3 视图级组件 ItemsTable） -->
      <ItemsTable v-else :items="detailItems" />
    </DetailDrawer>
  </section>
</template>

<style scoped>
.page-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 18px; }
.page-title { font-size: var(--font-title-1); font-weight: var(--weight-title); border-left: 4px solid var(--gold); padding-left: 12px; }
/* 页面副标题是正文档（16px），不是脚注。 */
.page-sub { margin-top: 6px; font-size: var(--font-body); color: var(--text-3); }
.alert { padding: 12px 16px; margin-bottom: 16px; border-radius: var(--radius-ctrl); border: 1px solid var(--border); font-size: var(--font-body); }
.alert--danger { border-color: var(--danger); color: var(--danger); }

/* 订阅源 URL：辅助档 13.5px（URL 是路径原文，不是主信息）。 */
.cnt.done { color: var(--success); }
.cnt.queued { color: var(--info); }
.cnt.failed { color: var(--danger); }
.cnt.skip { color: var(--text-3); }
/* 元信息（上次检查/下次/画质/周期）：辅助档 13.5px。 */
.meta > div { display: flex; gap: 10px; font-size: var(--font-aux); }
.meta dt { color: var(--text-3); width: 60px; flex-shrink: 0; }
.meta dd { color: var(--text-2); }

/* 表单字段名：正文档 16px。 */
.field input, .field select, .field textarea {
  padding: 9px 12px; border: 1px solid var(--border); border-radius: var(--radius-ctrl);
  background: var(--bg); color: var(--text-1); font-size: var(--font-body); font-family: inherit;
  width: 100%; box-sizing: border-box;
}
.field textarea { resize: vertical; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.field input:focus, .field select:focus, .field textarea:focus { outline: 2px solid var(--gold); outline-offset: 1px; }
.pv-meta > div { display: flex; gap: 10px; font-size: var(--font-aux); }
.pv-meta dt { color: var(--text-3); width: 44px; }

/* 追更到货横幅：surprise 表情 + 数字事实（方案 §5）。 */

/* 表格：表头 caption(12px)，表体 body(16px)。
   原来整张表都是 12px，表格密密麻麻一坨，完全看不出是数据表。 */
.tbl th { text-align: left; color: var(--text-3); font-weight: var(--weight-caption); font-size: var(--font-caption); padding: 8px 10px; border-bottom: 1px solid var(--border); }
.tbl td { padding: 9px 10px; border-bottom: 1px solid var(--border); color: var(--text-2); }
</style>