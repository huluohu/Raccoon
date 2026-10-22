<script setup lang="ts">
// QuickSubmit：全局快速提交（方案 §3「全局：顶部 QuickSubmit（`/` 快捷键聚焦）」）。
//
// 为什么抽成组件而不是留在 App.vue 里：
// 1) 方案 §3 把它列为全局关键组件，它同时出现在顶栏和下载页，两处逻辑必须一致
//    （同一个提交接口、同一套错误展示、同一个 `/` 聚焦语义）。留在 App.vue 里
//    就只能写两遍，改一处忘一处是迟早的事。
// 2) 组件对外只暴露两个口子：v-model（输入框内容）与 @submit（提交意图）。
//    它自己不调接口、不弹 toast——那是调用方的事。
//    组件管「长什么样、怎么聚焦」，上层管「提交之后干什么」，职责清楚。
// 3) 暴露 focus() 方法给 `/` 快捷键用（§7）：父组件拿 ref 调 focus()，
//    不需要组件把 input 暴露成 template ref 给外面乱摸。
// 4) 提交前先做格式校验（utils/link.ts）：分享文案粘进来能自动提取链接，
//    缺协议头 / 没链接时当场给提示，不用等一个网络往返才知道错在哪。
import { computed, ref } from 'vue'
import { checkLink } from '@/utils/link'

const props = withDefaults(
  defineProps<{
    /** 输入框内容。 */
    modelValue: string
    /** 占位文案。 */
    placeholder?: string
    /** 提交中：禁用输入与按钮。 */
    submitting?: boolean
    /** 错误文案：有值时在输入框下方展示（§7「错误卡+重试」，不展示空白）。 */
    error?: string
    /** 输入框最大宽度（px）。顶栏限宽，页面内可放宽。 */
    maxWidth?: number
    /** 按钮文案。 */
    buttonText?: string
    /** 输入框 id：同一页面可能有多个提交框，id 必须可区分（`/` 快捷键靠它聚焦）。 */
    inputId?: string
    /**
     * 操作区布局：
     *   'inline'（默认）按钮与输入框同一行——顶栏用，横向空间要留给通知按钮。
     *   'row'     按钮另起一行——下载页用：三个动作按钮挤在同一行会把
     *              URL 输入框压到 200px，长链接根本看不全。
     */
    actionsLayout?: 'inline' | 'row'
  }>(),
  {
    placeholder: '粘贴链接下载（回车直接提交）',
    submitting: false,
    error: '',
    maxWidth: 640,
    buttonText: '下载',
    inputId: 'quick-submit',
    actionsLayout: 'inline',
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'submit', value: string): void
}>()

defineSlots<{
  /** 替换默认的「下载」按钮。下载页要放「解析预览 / 直接下载」两枚，用它接。 */
  actions(props: { submit: () => void; disabled: boolean }): unknown
}>()

const inputEl = ref<HTMLInputElement | null>(null)

/**
 * 格式校验提示（本地判断，不发请求）。
 * 只有用户已经动过输入框才提示：页面刚加载时输入框是空的，
 * 挂一句「先粘贴一个链接吧」在下面纯属噪音。
 */
const touched = ref(false)
const formatError = computed(() => {
  if (!touched.value) return ''
  if (!props.modelValue.trim()) return ''
  const r = checkLink(props.modelValue)
  return r.ok ? '' : r.message
})

/** 聚焦输入框：给 `/` 快捷键（§7）与「点击提交后继续输入」用。 */
function focus(): void {
  inputEl.value?.focus()
  inputEl.value?.select()
}

defineExpose({ focus })

function onInput(ev: Event): void {
  touched.value = true
  emit('update:modelValue', (ev.target as HTMLInputElement).value)
}

/**
 * 提交前先过格式闸。
 * 通过时抛的是**提取后**的链接——分享文案里的裸链接会被规范化成纯 URL，
 * 引擎拿到干净地址，不必自己去啃一整段中文。
 */
function submit(): void {
  const target = props.modelValue.trim()
  if (!target || props.submitting) return
  touched.value = true
  const r = checkLink(target)
  if (!r.ok) return
  emit('submit', r.url)
}
</script>

<template>
  <div class="qsubmit" :style="{ maxWidth: maxWidth + 'px' }">
    <div class="qsubmit-row">
      <input
        ref="inputEl"
        :id="inputId"
        class="qsubmit-input"
        type="url"
        :value="modelValue"
        :placeholder="placeholder"
        :disabled="submitting"
        aria-label="粘贴链接下载"
        @input="onInput"
        @keyup.enter="submit"
      />
      <template v-if="actionsLayout === 'inline'">
        <slot name="actions" :submit="submit" :disabled="submitting || !modelValue.trim()">
          <button class="qsubmit-btn" type="button" :disabled="submitting || !modelValue.trim()" @click="submit">
            {{ submitting ? '提交中…' : buttonText }}
          </button>
        </slot>
      </template>
    </div>
    <!-- actionsLayout='row'：动作按钮另起一行，输入框独占整行宽度。 -->
    <div v-if="actionsLayout === 'row'" class="qsubmit-ops">
      <slot name="actions" :submit="submit" :disabled="submitting || !modelValue.trim()" />
    </div>
    <!-- 错误内联在输入框正下方，不弹浮层：提交失败时用户的视线还在输入框附近。
         格式错误（本地判断）优先于接口错误：格式错的时候根本发不出请求，
         拿接口错误位显示它只会误导。 -->
    <p v-if="formatError || error" class="qsubmit-err" role="alert">{{ formatError || error }}</p>
  </div>
</template>

<style scoped>
.qsubmit {
  flex: 1;
  min-width: 0;
}
.qsubmit-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.qsubmit-input {
  flex: 1;
  min-width: 0;
  padding: 9px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--bg);
  color: var(--text-1);
  font-size: var(--font-body);
}
.qsubmit-input:focus {
  outline: 2px solid var(--gold);
  outline-offset: 1px;
}
.qsubmit-input:disabled {
  background: var(--surface-2);
  color: var(--text-3);
  cursor: not-allowed;
}
.qsubmit-btn {
  flex-shrink: 0;
  padding: 9px 18px;
  border: none;
  border-radius: var(--radius-ctrl);
  background: var(--accent);
  color: var(--on-accent);
  font-size: var(--font-body);
  font-weight: var(--weight-semibold);
  cursor: pointer;
  transition: background 120ms ease-out;
}
.qsubmit-btn:hover:not(:disabled) {
  background: var(--accent-strong);
}
.qsubmit-btn:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.qsubmit-ops {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 10px;
}
.qsubmit-err {
  margin: 6px 0 0;
  color: var(--danger);
  font-size: var(--font-aux);
}

/* <768：输入框与按钮占满整行，按钮进触控尺寸（§6 ≥44px）。 */
@media (max-width: 767px) {
  .qsubmit {
    max-width: none;
  }
  .qsubmit-btn {
    min-height: 44px;
  }
}
</style>
