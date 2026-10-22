<script setup lang="ts">
// SubForm：新建订阅表单（方案 §3「/subscriptions → SubForm（先预览后创建）」）。
//
// 为什么抽成组件：
// 「先预览后创建」是这个表单的核心规则——不解析成功就不让保存。
// 这条规则必须集中在一处，不能散在视图的按钮 disabled 条件里：
// 之前它散在两处（预览按钮 + 保存按钮），改一处忘一处就会出现
// 「没预览也能保存」的洞。
//
// 组件自己不发请求：解析与保存由父组件执行（数据流走 api 层，§8 约定），
// 组件只负责收集表单值 + 把「能不能保存」这个判断说出来。
import { computed, reactive, watch } from 'vue'
import RButton from '@/components/base/RButton.vue'
import type { ParsePreview } from '@/api/types'

export interface SubFormValue {
  source_url: string
  source_type: string
  mode: string
  quality: string
  save_rule: string
  interval_min: number
  range_from: number
  range_to: number
  credential_id: string
}

const props = withDefaults(
  defineProps<{
    /** 解析预览结果（null = 还没解析）。 */
    preview?: ParsePreview | null
    /** 解析中。 */
    previewing?: boolean
    /** 保存中。 */
    saving?: boolean
    /** 初始值（打开抽屉时重置成这个）。 */
    initial?: Partial<SubFormValue>
  }>(),
  { preview: null, previewing: false, saving: false, initial: () => ({}) },
)

const emit = defineEmits<{
  /** 请求解析预览。 */
  preview: []
  /** 提交保存（值已在内部校验过）。 */
  save: [value: SubFormValue]
  /** 取消。 */
  cancel: []
}>()

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

const DEFAULT_VALUE: SubFormValue = {
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

const form = reactive<SubFormValue>({ ...DEFAULT_VALUE, ...props.initial })

/** 外部 initial 变化时重置（父组件每次打开抽屉都重置成默认值）。 */
watch(
  () => props.initial,
  (v) => {
    Object.assign(form, { ...DEFAULT_VALUE, ...(v ?? {}) })
  },
)

const rangeVisible = computed(() => form.mode === 'range')

/**
 * 「能不能保存」的唯一判定处：必须先解析成功。
 * 这条规则以前散在两处按钮条件里，现在只有这一处。
 */
const canSave = computed(() => props.preview !== null && !props.saving && !props.previewing)

function onSubmit() {
  if (!canSave.value) return
  emit('save', { ...form })
}

/**
 * 暴露当前来源链接给父组件（解析预览要用它）。
 * 表单值留在组件内部，但「解析预览」这个动作由父组件执行（走 api 层），
 * 所以父组件需要一个只读入口——暴露一个计算值，不暴露整个 form 对象。
 */
const sourceUrl = computed(() => form.source_url)

defineExpose({ sourceUrl })
</script>

<template>
  <form class="form" @submit.prevent="onSubmit">
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
      <RButton size="sm" :disabled="previewing" @click="emit('preview')">
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
          <dd>
            {{ preview.is_playlist ? '集合（' + (preview.formats?.length ?? 0) + ' 项）' : '单视频' }}
          </dd>
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
      <RButton variant="ghost" @click="emit('cancel')">取消</RButton>
      <RButton variant="accent" type="submit" :disabled="!canSave">
        {{ saving ? '保存中…' : preview ? '创建订阅' : '请先解析预览' }}
      </RButton>
    </div>
  </form>
</template>

<style scoped>
.form { display: flex; flex-direction: column; gap: 16px; }
.field { display: flex; flex-direction: column; gap: 6px; }
/* 表单字段名：正文档 16px。 */
.field-label { font-size: var(--font-body); font-weight: var(--weight-semibold); color: var(--text-2); }
.field input,
.field select,
.field textarea {
  padding: 9px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--bg);
  color: var(--text-1);
  font-size: var(--font-body);
  font-family: inherit;
  width: 100%;
  box-sizing: border-box;
}
.field textarea {
  resize: vertical;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.field input:focus,
.field select:focus,
.field textarea:focus {
  outline: 2px solid var(--gold);
  outline-offset: 1px;
}
.field-hint { font-size: var(--font-aux); color: var(--text-3); }
.preview-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.preview-box {
  padding: 12px 14px;
  border-radius: var(--radius-ctrl);
  background: var(--surface-2);
  border: 1px solid var(--border);
}
.pv-title { font-size: var(--font-body); font-weight: var(--weight-medium); margin-bottom: 8px; }
.pv-meta { display: flex; flex-direction: column; gap: 4px; }
.pv-meta > div { display: flex; gap: 10px; font-size: var(--font-aux); }
.pv-meta dt { color: var(--text-3); width: 44px; }
.range-row { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.form-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding-top: 10px;
  border-top: 1px solid var(--border);
}
</style>
