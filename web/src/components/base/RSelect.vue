<script setup lang="ts">
// RSelect：标签常驻 + 错误内联定位的下拉选择（docs/07 §4）。
//
// 为什么用原生 select 而不是自绘下拉：
// 原生控件自带键盘导航、触屏选择器、读屏软件语义和「点外面自动收起」，
// 自绘下拉要重新实现这些还得处理焦点陷阱，收益远小于成本。
// 视觉上只改外观（边框/圆角/焦点环），行为完全交给浏览器。
//
// 值类型：options 的 value 允许 string | number，但原生 select 只能传字符串，
// 所以内部用 String(value) 渲染 option，回读时再映射回原始类型，
// 避免「数字选项被静默变成字符串」这种上游拿到的脏数据。
import { computed, useId } from 'vue'

export interface RSelectOption {
  value: string | number
  label: string
  disabled?: boolean
}

/** 占位项哨兵值：业务里不会出现的字符串，避免和真实 option 的 value 撞车。 */
const PLACEHOLDER = '__rselect_placeholder__'
/** 「当前值没有对应 option」时的兜底项哨兵，保证 DOM 显示与 model 始终一致。 */
const NO_MATCH = '__rselect_no_match__'

const props = withDefaults(
  defineProps<{
    /** v-model 值，需与某个 option 的 value 同类型。 */
    modelValue?: string | number
    /** 选项列表。 */
    options: RSelectOption[]
    /** 字段标签，常驻显示在选择框上方。 */
    label: string
    /** 未选中时展示的占位文案。 */
    placeholder?: string
    /** 内联错误信息；非空时描边转 danger 色。 */
    error?: string
    /** 辅助说明（与 error 互斥，error 优先展示）。 */
    hint?: string
    disabled?: boolean
    required?: boolean
    /** 是否显示清除按钮。 */
    clearable?: boolean
    /** 浏览器自动填充策略，透传给原生 select。 */
    autocomplete?: string
    name?: string
    id?: string
  }>(),
  {
    modelValue: '',
    options: () => [],
    placeholder: '',
    error: '',
    hint: '',
    disabled: false,
    required: false,
    clearable: false,
    autocomplete: 'off',
    name: '',
    id: '',
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: string | number): void
  (e: 'clear'): void
}>()

const uid = useId()

const selectId = computed(() => props.id || uid)
const errorId = computed(() => `${selectId.value}-err`)
const hintId = computed(() => `${selectId.value}-hint`)

const hasValue = computed(() => props.modelValue !== '' && props.modelValue !== null && props.modelValue !== undefined)
const hasMatch = computed(() => props.options.some((o) => String(o.value) === String(props.modelValue)))
const showClear = computed(() => props.clearable && !props.disabled && hasValue.value)

const describedBy = computed(() => {
  if (props.error) return errorId.value
  if (props.hint) return hintId.value
  return undefined
})

const cls = computed(() => [
  'rselect',
  { 'rselect--error': !!props.error, 'rselect--disabled': props.disabled },
])

/**
 * 原生 select 只能承载字符串，这里做一次「字符串 ↔ 原始类型值」的双向映射：
 * 读的时候把 modelValue 转成字符串去匹配 option，写的时候把 DOM 值还原成 option 的原始 value。
 * 匹配不上或没值时落到哨兵项上，保证「屏幕上显示的」和「model 里的」永远是同一个东西。
 */
const innerValue = computed<string>({
  get: () => {
    if (hasValue.value && hasMatch.value) return String(props.modelValue)
    if (!hasValue.value && props.placeholder) return PLACEHOLDER
    return NO_MATCH
  },
  set: (v: string) => {
    if (v === PLACEHOLDER || v === NO_MATCH) return
    const hit = props.options.find((o) => String(o.value) === v)
    if (hit) emit('update:modelValue', hit.value)
  },
})

function onClear() {
  emit('update:modelValue', '')
  emit('clear')
}
</script>

<template>
  <div :class="cls">
    <label class="rselect-label" :for="selectId">{{ label }}</label>
    <div class="rselect-wrap">
      <select
        :id="selectId"
        v-model="innerValue"
        class="rselect-field"
        :class="{ 'rselect-field--error': !!error }"
        :disabled="disabled"
        :required="required"
        :autocomplete="autocomplete"
        :name="name || undefined"
        :aria-invalid="error ? 'true' : undefined"
        :aria-describedby="describedBy"
      >
        <!-- 占位项：未选中且给了 placeholder 时展示，占位文案本身就是用户要找的提示。 -->
        <option v-if="!hasValue && placeholder" :value="PLACEHOLDER" disabled>{{ placeholder }}</option>
        <!-- 兜底项：保证「屏幕显示的」和「model 里的」永远是同一个东西。
             没值时它必须 hidden，否则会在下拉列表里多出一条空白项； -->
        <option v-else-if="!hasValue || !hasMatch" :value="NO_MATCH" disabled :hidden="!hasValue">
          {{ hasValue ? String(modelValue) : '' }}
        </option>
        <option v-for="o in options" :key="String(o.value)" :value="String(o.value)" :disabled="o.disabled">
          {{ o.label }}
        </option>
      </select>
      <button
        v-if="showClear"
        class="rselect-clear"
        type="button"
        :aria-label="`清除${label}`"
        @click="onClear"
      >
        ✕
      </button>
    </div>
    <p v-if="error" :id="errorId" class="rselect-msg rselect-msg--error" role="alert">{{ error }}</p>
    <p v-else-if="hint" :id="hintId" class="rselect-msg">{{ hint }}</p>
  </div>
</template>

<style scoped>
.rselect {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.rselect-label {
  font-size: var(--font-aux);
  font-weight: 500;
  color: var(--text-2);
}
.rselect-wrap {
  position: relative;
  display: flex;
  align-items: center;
}
.rselect-field {
  width: 100%;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--surface);
  color: var(--text-1);
  font-size: var(--font-body);
  font-family: inherit;
  cursor: pointer;
  transition: border-color 120ms ease-out;
}
.rselect-field:focus {
  outline: 2px solid var(--gold);
  outline-offset: 1px;
}
.rselect-field:disabled {
  background: var(--surface-2);
  color: var(--text-3);
  cursor: not-allowed;
}
.rselect-field--error {
  border-color: var(--danger);
}
.rselect-field--error:focus {
  outline-color: var(--danger);
}
.rselect-clear {
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
  font-size: 12px;
  line-height: 1;
  cursor: pointer;
  transition: background 120ms ease-out, color 120ms ease-out;
}
.rselect-clear:hover {
  background: var(--surface-3);
  color: var(--text-1);
}
.rselect-msg {
  margin: 0;
  font-size: var(--font-caption);
  color: var(--text-3);
}
.rselect-msg--error {
  color: var(--danger);
}
</style>
