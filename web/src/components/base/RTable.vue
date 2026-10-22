<script setup lang="ts" generic="T extends object = Record<string, unknown>">
// RTable：数据表（docs/07 §4「表头浅底；行高 44/36；hover 行操作；空态 ≠ 骨架屏」）。
//
// 设计取舍：
// 1) 用语义化 table：表头/表体天然可被读屏软件识别为表格，比 div 网格省一半无障碍工作。
// 2) 行高两档：size='md' 行高 44px（默认，触控友好），size='sm' 行高 36px（紧凑列表）。
// 3) 行内操作区默认隐藏、hover（或键盘聚焦）才出现——表格主体是数据，操作是次要信息，
//    常驻会把每行的注意力都拉走；但必须能被键盘触达，所以用 focus-within 一起兜住。
// 4) 空态与骨架屏是两个互斥状态：loading 时只出骨架，rows 为空且不 loading 时才出 empty 插槽，
//    绝不在同一时刻同时出现（「正在加载」和「没有数据」是互斥的事实）。
// 5) 列定义走 columns props（key/title/width/align），单元格内容可用 `cell-列key` 具名插槽覆盖；
//    两者共存，静态列直接给 key 就行，需要富内容（状态点、进度条）的列再补插槽。
//
// 组件对行类型做泛型化（T）：插槽里的 row 就是调用方传进来的那个类型，
// 不用在上层反复 as 断言，改表格列时也不会因为行结构变了而漏改。
// T 的约束用 object 而不是 Record<string, unknown>：后者会拒收 interface 定义的行类型
// （interface 没有隐式索引签名），而本项目的行类型全是 interface。
import { computed } from 'vue'
import { skeletonWidths, SKELETON_ROW_COUNT } from './skeleton'

export interface RTableColumn {
  /** 对应行对象的字段名。 */
  key: string
  /** 表头文案。 */
  title: string
  /** 列宽，任意 CSS 长度（'120px' / '20%'）。 */
  width?: string
  /** 单元格对齐方式。 */
  align?: 'left' | 'center' | 'right'
}

const props = withDefaults(
  defineProps<{
    /** 行数据数组。 */
    rows: T[]
    /** 列定义。 */
    columns: RTableColumn[]
    /** 行高档位：md=44px（默认）/ sm=36px。 */
    size?: 'md' | 'sm'
    /** 加载中：渲染骨架行，且不渲染空态（骨架与空态互斥）。 */
    loading?: boolean
    /** 骨架行数，loading 时展示。 */
    skeletonRows?: number
    /** 行主键字段名，用于 v-for 的 key（保证 diff 稳定）。 */
    rowKey?: string
    /** 无数据时的兜底文案（未提供 empty 插槽时使用）。 */
    emptyText?: string
  }>(),
  {
    rows: () => [],
    columns: () => [],
    size: 'md',
    loading: false,
    skeletonRows: SKELETON_ROW_COUNT,
    rowKey: 'id',
    emptyText: '暂无数据',
  },
)

const emit = defineEmits<{
  (e: 'row-click', row: T, index: number): void
}>()

defineSlots<{
  /** 行内操作区（hover / 键盘聚焦该行时出现）。 */
  actions(props: { row: T; index: number }): unknown
  /** 无数据空态。与骨架屏互斥。 */
  empty(): unknown
  /** 单元格内容覆盖，具名规则为 `cell-<列 key>`。 */
  [key: string]: (props: { row: T; value: unknown; index: number }) => unknown
}>()

const cls = computed(() => ['rtable', `rtable--${props.size}`])

/** 列宽映射到 colgroup：宽度只写一次，表头表体自动对齐。 */
const colWidths = computed(() => props.columns.map((c) => c.width))

/** 行 key：优先取 rowKey 字段，取不到就退回索引，保证 v-for 的 key 始终是原始值。 */
function keyOf(row: T, index: number): string | number {
  const v = Reflect.get(row, props.rowKey)
  return typeof v === 'string' || typeof v === 'number' ? v : index
}

/** 取单元格原始值：走 Reflect.get，值类型是 unknown，展示成什么由插槽/上层决定。 */
function cellValue(row: T, key: string): unknown {
  return Reflect.get(row, key)
}

/** 单元格插槽名规则：`cell-<列 key>`。 */
function cellSlot(key: string): string {
  return `cell-${key}`
}

/** 兜底渲染：对象/数组原样序列化，其余转字符串。 */
function display(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

/** 骨架每行的灰块宽度：不等宽，避免看起来像真数据。 */
function skeletonRow(index: number): number[] {
  return skeletonWidths(props.columns.length, index)
}

function onRowClick(row: T, index: number) {
  emit('row-click', row, index)
}
</script>

<template>
  <div class="rtable-wrap">
    <table :class="cls">
      <colgroup>
        <col v-for="(w, i) in colWidths" :key="i" :style="w ? { width: w } : undefined" />
      </colgroup>
      <thead>
        <tr>
          <th v-for="c in columns" :key="c.key" :class="c.align ? `rtable-cell--${c.align}` : undefined">
            {{ c.title }}
          </th>
          <th class="rtable-cell rtable-cell--actions" scope="col">
            <span class="rtable-sr">操作</span>
          </th>
        </tr>
      </thead>
      <tbody>
        <!-- 骨架屏：loading 时唯一的内容状态，此时不渲染空态（两者互斥）。 -->
        <template v-if="loading">
          <tr v-for="n in skeletonRows" :key="`skeleton-${n}`" class="rtable-row rtable-row--skeleton">
            <td v-for="(w, ci) in skeletonRow(n)" :key="ci" class="rtable-cell">
              <span class="rtable-skeleton" :style="{ width: `${w}%` }" />
            </td>
            <td class="rtable-cell rtable-cell--actions" />
          </tr>
        </template>

        <!-- 空态：仅在非加载且确实没有数据时出现。 -->
        <tr v-else-if="rows.length === 0" class="rtable-row rtable-row--empty">
          <td :colspan="columns.length + 1" class="rtable-cell rtable-cell--empty">
            <slot name="empty">
              <span class="rtable-empty-text">{{ emptyText }}</span>
            </slot>
          </td>
        </tr>

        <template v-else>
          <tr
            v-for="(row, index) in rows"
            :key="keyOf(row, index)"
            class="rtable-row"
            @click="onRowClick(row, index)"
          >
            <td v-for="c in columns" :key="c.key" :class="c.align ? `rtable-cell--${c.align}` : undefined">
              <slot :name="cellSlot(c.key)" :row="row" :value="cellValue(row, c.key)" :index="index">
                {{ display(cellValue(row, c.key)) }}
              </slot>
            </td>
            <td class="rtable-cell rtable-cell--actions">
              <slot name="actions" :row="row" :index="index" />
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.rtable-wrap {
  width: 100%;
  overflow-x: auto;
}
.rtable {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-aux);
  color: var(--text-1);
}
/* 表头浅底：和表体拉开层次，但不用重色，避免抢数据的注意力。 */
.rtable th {
  padding: 8px 12px;
  background: var(--surface-2);
  color: var(--text-2);
  font-size: var(--font-caption);
  font-weight: var(--weight-caption);
  text-align: left;
  white-space: nowrap;
  border-bottom: 1px solid var(--border);
}
.rtable-cell {
  padding: 0 12px;
  color: var(--text-1);
  vertical-align: middle;
}
/* 行高两档：44px 触控友好，36px 紧凑列表。 */
.rtable--md .rtable-row {
  height: 44px;
}
.rtable--sm .rtable-row {
  height: 36px;
}
.rtable-row {
  border-bottom: 1px solid var(--border);
}
.rtable-row:hover {
  background: var(--surface-2);
}
.rtable-cell--center {
  text-align: center;
}
.rtable-cell--right {
  text-align: right;
}
.rtable-cell--actions {
  width: 1%;
  text-align: right;
  white-space: nowrap;
}
/* 操作区默认隐藏，hover 该行或键盘聚焦该行时出现。 */
.rtable-cell--actions > :deep(*) {
  opacity: 0;
  transition: opacity 120ms ease-out;
}
.rtable-row:hover .rtable-cell--actions > :deep(*),
.rtable-row:focus-within .rtable-cell--actions > :deep(*) {
  opacity: 1;
}
.rtable-row--empty .rtable-cell--empty {
  padding: 32px 12px;
  text-align: center;
}
.rtable-empty-text {
  color: var(--text-3);
  font-size: var(--font-aux);
}
.rtable-skeleton {
  display: block;
  height: 12px;
  border-radius: var(--radius-ctrl);
  background: var(--surface-3);
}
.rtable-sr {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
  border: 0;
}
</style>
