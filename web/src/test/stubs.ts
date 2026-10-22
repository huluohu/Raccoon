import { h } from 'vue'
import type { Component } from 'vue'

/**
 * 视图单测的通用 stub。
 *
 * 为什么要集中定义：stub 必须显式声明 props，否则 Vue 会把父组件传下来的
 * props 当成 DOM attribute 设置，刷一屏 "Failed setting prop" 告警，
 * 把真正的失败信号淹掉。这里统一声明，各 spec 直接复用。
 */

/** RButton：保留 type/disabled 语义（type=submit 才能触发表单提交）。 */
export const RButtonStub: Component = {
  props: {
    variant: { type: String, default: 'outline' },
    tone: { type: String, default: 'default' },
    size: { type: String, default: 'md' },
    disabled: { type: Boolean, default: false },
    confirm: { type: String, default: '' },
    type: { type: String, default: 'button' },
    block: { type: Boolean, default: false },
  },
  emits: ['click'],
  setup(props, { slots, emit }) {
    return () =>
      h(
        'button',
        { type: props.type, disabled: props.disabled, onClick: (e: MouseEvent) => emit('click', e) },
        slots.default?.(),
      )
  },
}

/** RSelect：原生 select 语义，v-model 双向可用。 */
export const RSelectStub: Component = {
  props: {
    modelValue: { type: [String, Number], default: '' },
    options: { type: Array, default: () => [] },
    label: { type: String, default: '' },
    error: { type: String, default: '' },
    hint: { type: String, default: '' },
    disabled: { type: Boolean, default: false },
    required: { type: Boolean, default: false },
    clearable: { type: Boolean, default: false },
  },
  emits: ['update:modelValue', 'clear'],
  setup(props, { emit }) {
    return () =>
      h(
        'select',
        {
          'aria-label': props.label,
          onChange: (e: Event) => emit('update:modelValue', (e.target as HTMLSelectElement).value),
        },
        (props.options as { value: string | number; label: string }[]).map((o) =>
          h('option', { value: o.value }, o.label),
        ),
      )
  },
}

/** RInput：保留 v-model 与 label。 */
export const RInputStub: Component = {
  props: {
    modelValue: { type: [String, Number], default: '' },
    label: { type: String, default: '' },
    type: { type: String, default: 'text' },
    error: { type: String, default: '' },
    hint: { type: String, default: '' },
    disabled: { type: Boolean, default: false },
    readonly: { type: Boolean, default: false },
    required: { type: Boolean, default: false },
    clearable: { type: Boolean, default: true },
    placeholder: { type: String, default: '' },
    autocomplete: { type: String, default: 'off' },
  },
  emits: ['update:modelValue', 'paste', 'clear'],
  setup(props, { emit }) {
    return () =>
      h('input', {
        'aria-label': props.label,
        value: props.modelValue as string,
        onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value),
      })
  },
}

/** router-link：视图里用来跳转的链接；stub 成普通 a 标签即可。 */
export const RouterLinkStub: Component = {
  props: { to: { type: [String, Object], default: '' } },
  setup: (props, { slots }) => () =>
    h('a', { href: typeof props.to === 'string' ? props.to : '#' }, slots.default?.()),
}

/** 数据表：真实渲染 table/thead/tbody 与行，供断言表头与行数据。 */
export const RTableStub: Component = {
  props: {
    rows: { type: Array, default: () => [] },
    columns: { type: Array, default: () => [] },
    size: { type: String, default: 'md' },
    loading: { type: Boolean, default: false },
    skeletonRows: { type: Number, default: 3 },
    rowKey: { type: String, default: 'id' },
    emptyText: { type: String, default: '暂无数据' },
  },
  emits: ['row-click'],
  setup(props, { slots, emit }) {
    const cols = props.columns as { key: string; title: string }[]
    return () =>
      h('table', { class: 'stub-table' }, [
        h('thead', null, [h('tr', null, cols.map((c) => h('th', null, c.title)))]),
        h(
          'tbody',
          null,
          (props.rows as Record<string, unknown>[]).map((row, i) =>
            h(
              'tr',
              { onClick: () => emit('row-click', row, i) },
              cols.map((c) => h('td', null, [slots[`cell-${c.key}`]?.({ row, value: row[c.key], index: i }) ?? String(row[c.key] ?? '')])),
            ),
          ),
        ),
      ])
  },
}

/** DetailDrawer：打开即渲染内容（含 title，供断言「新建/编辑」态切换）。 */
export const DetailDrawerStub: Component = {
  props: { open: { type: Boolean, default: false }, title: { type: String, default: '' } },
  emits: ['close'],
  setup(props, { slots }) {
    return () =>
      props.open
        ? h('div', { class: 'stub-drawer' }, [
            h('p', { class: 'stub-drawer-title' }, props.title),
            slots.default?.(),
          ])
        : null
  },
}

/** REmpty：保留 face/title/hint/actionLabel 文案。 */
export const REmptyStub: Component = {
  props: {
    face: { type: String, default: 'waiting' },
    title: { type: String, default: '' },
    hint: { type: String, default: '' },
    actionLabel: { type: String, default: '' },
  },
  emits: ['action'],
  setup(props, { slots }) {
    return () =>
      h('div', { class: 'stub-empty', 'data-face': props.face }, [
        props.title,
        props.hint ? h('span', { class: 'stub-empty-hint' }, props.hint) : null,
        props.actionLabel ? h('span', { class: 'stub-empty-action' }, props.actionLabel) : null,
        slots.default?.(),
      ])
  },
}

/** 平台徽章：只留一个标记类，供断言存在性。 */
export const PlatformBadgeStub: Component = {
  props: { platform: { type: String, default: '' }, compact: { type: Boolean, default: false } },
  setup: (props) => () => h('i', { class: 'stub-badge', 'data-platform': props.platform }),
}

/** 七态色点：同上。 */
export const StatusDotStub: Component = {
  props: { status: { type: String, default: '' } },
  setup: (props) => () => h('i', { class: 'stub-dot', 'data-status': props.status }),
}

/** 进度条：同上。 */
export const TaskProgressStub: Component = {
  props: { task: { type: Object, default: () => ({}) }, compact: { type: Boolean, default: false } },
  setup: () => () => h('div', { class: 'stub-prog' }),
}

/** 浣熊表情：同上（禁止在断言里依赖 emoji）。 */
export const RaccoonFaceStub: Component = {
  props: { face: { type: String, default: 'waiting' }, size: { type: Number, default: 48 } },
  setup: (props) => () => h('i', { class: 'stub-face', 'data-face': props.face }),
}
