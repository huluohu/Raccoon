<script setup lang="ts">
// RecordPlanForm：添加直播源表单（方案 §3「/live → RecordPlanForm」）。
//
// 为什么抽成组件：
// 1) 「不支持的平台在下拉里禁用并说明原因」是这一页的铁律——
//    支持清单由后端下发（实测清单），前端不写死。禁用的判断和说明文案
//    必须在组件里成对出现，否则会出现「禁用了但没说为什么」。
// 2) 表单字段多（平台 / 地址 / 备注 / 画质 / 计划 / 分段 / 检测间隔），
//    留在视图里会把视图撑成 400 行。
import { computed, reactive, watch } from 'vue'
import RButton from '@/components/base/RButton.vue'

export interface LiveFormValue {
  platform: string
  room_url: string
  title: string
  quality: string
  record_plan: string
  segment_min: number
  check_interval_sec: number
  credential_id: string
}

const props = withDefaults(
  defineProps<{
    /** 后端下发的实测支持平台清单。 */
    supported?: string[]
    /** 清单说明（后端给的话术）。 */
    supportedNote?: string
    /** 保存中。 */
    saving?: boolean
    /** 初始值（打开抽屉时重置）。 */
    initial?: Partial<LiveFormValue>
  }>(),
  { supported: () => [], supportedNote: '', saving: false, initial: () => ({}) },
)

const emit = defineEmits<{
  save: [value: LiveFormValue]
  cancel: []
}>()

/** 平台中文名。 */
const PLATFORM_LABEL: Record<string, string> = {
  bilibili: '哔哩哔哩',
  douyu: '斗鱼',
  huya: '虎牙',
  kuaishou: '快手',
}

/** 录制计划选项（带说明文案）。 */
const PLANS = [
  { value: 'manual', label: '仅手动', hint: '只在点「开始录制」时录' },
  { value: 'always', label: '开播即录', hint: '检测到开播就自动录制' },
  { value: 'scheduled', label: '定时录制', hint: '按 schedule_json 设定的时段录' },
]

/** 前端候选平台（清单由后端决定谁可用）。 */
const ALL_PLATFORMS = ['bilibili', 'douyu', 'huya', 'kuaishou']

const DEFAULT_VALUE: LiveFormValue = {
  platform: 'bilibili',
  room_url: '',
  title: '',
  quality: 'best',
  record_plan: 'manual',
  segment_min: 30,
  check_interval_sec: 120,
  credential_id: '',
}

const form = reactive<LiveFormValue>({ ...DEFAULT_VALUE, ...props.initial })

watch(
  () => props.initial,
  (v) => {
    Object.assign(form, { ...DEFAULT_VALUE, ...(v ?? {}) })
  },
)

/** 平台是否在实测清单里：不在则下拉里禁用并标原因。 */
function platformDisabled(p: string): boolean {
  return !props.supported.includes(p)
}

/** 当前选的平台是否被禁用（后端清单变了、初始值指向不支持的平台）。 */
const currentPlatformInvalid = computed(
  () => form.platform !== '' && props.supported.length > 0 && platformDisabled(form.platform),
)

function onSubmit() {
  // 直播间地址必填：空地址提交上去后端只会报 422。
  if (!form.room_url.trim()) return
  emit('save', { ...form })
}
</script>

<template>
  <form class="form" @submit.prevent="onSubmit">
    <label class="field">
      <span class="field-label">平台</span>
      <select v-model="form.platform">
        <option
          v-for="p in ALL_PLATFORMS"
          :key="p"
          :value="p"
          :disabled="platformDisabled(p)"
        >
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

    <p v-if="currentPlatformInvalid" class="field-warn">
      选中的平台不在实测支持清单里，先换一个平台再添加。
    </p>

    <div class="form-foot">
      <RButton variant="ghost" @click="emit('cancel')">取消</RButton>
      <RButton
        variant="accent"
        type="submit"
        :disabled="saving || !form.room_url.trim() || currentPlatformInvalid"
      >
        {{ saving ? '保存中…' : '添加' }}
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
.field select {
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
.field input:focus,
.field select:focus {
  outline: 2px solid var(--gold);
  outline-offset: 1px;
}
.field-hint { font-size: var(--font-aux); color: var(--text-3); }
.field-warn { font-size: var(--font-aux); color: var(--warning); margin: 0; }
.two-col { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.form-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding-top: 10px;
  border-top: 1px solid var(--border);
}
</style>
