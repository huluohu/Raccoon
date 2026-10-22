<script setup lang="ts">
// RInput：标签常驻 + 错误内联 + 清除/粘贴反馈（docs/07 §4）。
//
// 设计取舍：
// 1) label 常驻在输入框上方（不是拿 placeholder 顶替）——placeholder 一旦有值就消失，
//    用户和读屏软件都会失去字段语义；label 常驻才能在填完值后仍知道这是哪个字段。
// 2) 错误信息内联在输入框正下方，且描边转 danger 色——表单校验失败时要让用户
//    「不用移动视线」就能把错误和出错的输入框对上。
// 3) 清除按钮只在「有值 && 非 password」时出现——password 的清除是误触高发区，
//    且清空后无法找回。
// 4) 粘贴事件以 emit 上抛给上层，由上层决定给什么反馈（提示「已粘贴」/走解析预览），
//    组件自己不吞事件，避免上层拿不到剪贴板内容。
import { computed, ref, useId } from 'vue'

export type RInputType = 'text' | 'password' | 'number' | 'url' | 'search' | 'email' | 'tel'

const props = withDefaults(
  defineProps<{
    /** v-model 值。文本类为 string，type=number 时为 number（清空时回空串）。 */
    modelValue?: string | number
    /** 输入类型。 */
    type?: RInputType
    /** 字段标签，常驻显示在输入框上方。 */
    label: string
    placeholder?: string
    /** 内联错误信息；非空时描边转 danger 色。 */
    error?: string
    /** 辅助说明（与 error 互斥，error 优先展示）。 */
    hint?: string
    disabled?: boolean
    readonly?: boolean
    required?: boolean
    /** 是否显示清除按钮（password 类型恒不显示）。 */
    clearable?: boolean
    /** 浏览器自动填充策略，透传给原生 input。 */
    autocomplete?: string
    name?: string
    id?: string
    maxlength?: number
    inputmode?: 'none' | 'text' | 'tel' | 'url' | 'email' | 'numeric' | 'decimal' | 'search'
  }>(),
  {
    modelValue: '',
    type: 'text',
    placeholder: '',
    error: '',
    hint: '',
    disabled: false,
    readonly: false,
    required: false,
    clearable: true,
    autocomplete: 'off',
    name: '',
    id: '',
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: string | number): void
  (e: 'paste', ev: ClipboardEvent): void
  (e: 'clear'): void
}>()

const uid = useId()
const inputEl = ref<HTMLInputElement | null>(null)

const inputId = computed(() => props.id || uid)
const errorId = computed(() => `${inputId.value}-err`)
const hintId = computed(() => `${inputId.value}-hint`)

/** 有值才允许清除；password 不提供清除（误触高发 + 清掉找不回）。 */
const showClear = computed(
  () => props.clearable && props.type !== 'password' && !props.disabled && !props.readonly && props.modelValue !== '',
)

const describedBy = computed(() => {
  if (props.error) return errorId.value
  if (props.hint) return hintId.value
  return undefined
})

const cls = computed(() => [
  'rinput',
  `rinput--${props.type}`,
  { 'rinput--error': !!props.error, 'rinput--disabled': props.disabled },
])

function onInput(ev: Event) {
  const raw = (ev.target as HTMLInputElement).value
  // number 类型回 number；清空时给空串而不是 0，避免把「没填」和「填了 0」混同。
  if (props.type === 'number') emit('update:modelValue', raw === '' ? '' : Number(raw))
  else emit('update:modelValue', raw)
}

/** 粘贴不拦截，只把原生事件上抛给上层做反馈。 */
function onPaste(ev: ClipboardEvent) {
  emit('paste', ev)
}

function onClear() {
  emit('update:modelValue', '')
  emit('clear')
  // 清除后把焦点还给输入框，用户可以接着打字（清完还要重新点一次很蠢）。
  inputEl.value?.focus()
}
</script>

<template>
  <div :class="cls">
    <label class="rinput-label" :for="inputId">{{ label }}</label>
    <div class="rinput-wrap">
      <input
        :id="inputId"
        ref="inputEl"
        class="rinput-field"
        :class="{ 'rinput-field--error': !!error }"
        :type="type"
        :value="modelValue"
        :placeholder="placeholder"
        :disabled="disabled"
        :readonly="readonly"
        :required="required"
        :autocomplete="autocomplete"
        :name="name || undefined"
        :maxlength="maxlength"
        :inputmode="inputmode"
        :aria-invalid="error ? 'true' : undefined"
        :aria-describedby="describedBy"
        @input="onInput"
        @paste="onPaste"
      />
      <button
        v-if="showClear"
        class="rinput-clear"
        type="button"
        :aria-label="`清除${label}`"
        @click="onClear"
      >
        ✕
      </button>
    </div>
    <p v-if="error" :id="errorId" class="rinput-msg rinput-msg--error" role="alert">{{ error }}</p>
    <p v-else-if="hint" :id="hintId" class="rinput-msg">{{ hint }}</p>
  </div>
</template>

<style scoped>
.rinput {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
/* label 常驻：填完值也保留字段语义，读屏软件也能把 label 与输入框绑定。 */
.rinput-label {
  font-size: var(--font-aux);
  font-weight: var(--weight-caption);
  color: var(--text-2);
}
.rinput-wrap {
  position: relative;
  display: flex;
  align-items: center;
}
.rinput-field {
  width: 100%;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--surface);
  color: var(--text-1);
  font-size: var(--font-body);
  font-family: inherit;
  transition: border-color 120ms ease-out;
}
.rinput-field::placeholder {
  color: var(--text-3);
}
.rinput-field:focus {
  outline: 2px solid var(--gold);
  outline-offset: 1px;
}
.rinput-field:disabled {
  background: var(--surface-2);
  color: var(--text-3);
  cursor: not-allowed;
}
.rinput-field--error {
  border-color: var(--danger);
}
.rinput-field--error:focus {
  outline-color: var(--danger);
}
/* 清除按钮占位绝对定位，右侧留出内边距，避免有值/无值时输入框宽度跳动。 */
.rinput-clear {
  position: absolute;
  right: 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  padding: 0;
  border: none;
  border-radius: var(--radius-pill);
  background: transparent;
  color: var(--text-3);
  font-size: var(--font-caption);
  line-height: 1;
  cursor: pointer;
  transition: background 120ms ease-out, color 120ms ease-out;
}
.rinput-clear:hover {
  background: var(--surface-3);
  color: var(--text-1);
}
.rinput-msg {
  margin: 0;
  font-size: var(--font-aux);
  color: var(--text-3);
}
.rinput-msg--error {
  color: var(--danger);
}
</style>
