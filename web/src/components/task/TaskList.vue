<script setup lang="ts">
// TaskList：下载页任务列表（方案 §3「/tasks → TaskList（筛选/批量工具条）」）。
//
// 为什么抽成组件：
// 1) 列表是下载页的骨架，但它的逻辑量（父子树 / 筛选 / 展开 / 多选 / 批量操作）
//    占了整个视图的大半。留在视图里，视图就变成 900 行的巨型文件，
//    任何改动都要在「提交区 + 预览区 + 批量面板 + 列表」之间来回跳。
// 2) 组件只吃 props、只抛事件：数据源仍在 store（§7 铁律「状态唯一来源」）。
// 3) 批量工具条是「筛选/批量工具条」这半句方案的另一半——原来页面上只有
//    键盘多选（Shift+Space），没有任何可见的批量入口，用户根本不知道能批量。
//
// 状态取用（§5）：空态用 waiting 表情（没有任务在等），由父组件传 face 进来。
import { computed } from 'vue'
import TaskCard from '@/components/task/TaskCard.vue'
import TaskRow from '@/components/task/TaskRow.vue'
import RButton from '@/components/base/RButton.vue'
import REmpty from '@/components/base/REmpty.vue'
import { isActiveStatus, type FilterKey, type FilterOption } from '@/composables/useTaskTree'
import type { Task } from '@/api/types'

const props = withDefaults(
  defineProps<{
    /** 当前筛选下可见的顶层任务。 */
    roots: Task[]
    /** 筛选项定义。 */
    filters: FilterOption[]
    /** 当前筛选（v-model:filter）。 */
    filter: FilterKey
    /** 各筛选项的计数（显示在标签右侧）。 */
    countOf: (key: FilterKey) => number
    /** 加载中。 */
    loading?: boolean
    /** 列表被服务端截断（只加载了最近 N 条）。 */
    truncated?: boolean
    /** 已选中的任务 ID（用于批量工具条与行选中态）。 */
    selectedIds?: Set<string>
    /** 展开的父任务 ID。 */
    expandedIds?: Set<string>
    /** 本轮新插入、需要高亮定位的任务 ID（§6 列表变更 180ms）。 */
    flashIds?: Set<string>
    /** 正在执行操作的任务 ID（按钮置灰）。 */
    busyId?: string
    /** 父任务子项统计查询。 */
    statOf: (id: string) => { total: number; failed: number; done: number }
    /** 父任务子项列表查询（展开时用）。 */
    childrenOf: (id: string) => Task[]
    /** 父任务展开后按筛选展示的子项。 */
    shownChildren: (id: string) => Task[]
    /** 是否为父任务。 */
    isParent: (t: Task) => boolean
  }>(),
  {
    loading: false,
    truncated: false,
    selectedIds: () => new Set<string>(),
    expandedIds: () => new Set<string>(),
    flashIds: () => new Set<string>(),
    busyId: '',
  },
)

const emit = defineEmits<{
  /** 切换筛选。 */
  'update:filter': [key: FilterKey]
  /** 打开任务详情。 */
  open: [task: Task]
  /** 行选中（带鼠标事件，Shift 表示追加）。 */
  select: [task: Task, ev?: MouseEvent]
  /** 展开/收起父任务子项。 */
  'toggle-expand': [id: string]
  /** 重试单个任务。 */
  retry: [id: string]
  /** 取消单个任务。 */
  cancel: [id: string]
  /** 删除单个任务（父组件负责弹确认框）。 */
  remove: [task: Task]
  /** 重试父任务的失败子项。 */
  'retry-failed-children': [parent: Task]
  /** 批量操作：动作 + 任务 ID 列表。 */
  batch: [action: 'cancel' | 'retry' | 'delete', ids: string[]]
}>()

/** 已选数量——批量工具条只有选了东西才出现（不占常态空间）。 */
const selectedCount = computed(() => props.selectedIds?.size ?? 0)

function isSelected(id: string): boolean {
  return props.selectedIds?.has(id) ?? false
}

function isExpanded(id: string): boolean {
  return props.expandedIds?.has(id) ?? false
}

/** 批量动作统一从选中集合取 ID，避免各处重复判空。 */
function runBatch(action: 'cancel' | 'retry' | 'delete'): void {
  const ids = Array.from(props.selectedIds ?? [])
  if (!ids.length) return
  emit('batch', action, ids)
}

/** 清空选择（批量操作完成后调用）。 */
function clearSelection(): void {
  for (const id of props.selectedIds ?? []) emit('select', { id } as Task)
}
</script>

<template>
  <div class="tasklist">
    <!-- 筛选工具条 -->
    <div class="filters" role="tablist" aria-label="任务筛选">
      <button
        v-for="f in filters"
        :key="f.key"
        type="button"
        role="tab"
        class="tab"
        :class="{ on: filter === f.key }"
        :aria-selected="filter === f.key"
        @click="emit('update:filter', f.key)"
      >
        {{ f.label }}<em class="tnum">{{ countOf(f.key) }}</em>
      </button>
    </div>

    <!-- 批量工具条：只有选中任务才出现。
         原来页面上只有 Shift+Space 键盘多选，没有任何可见入口——用户不知道能批量。 -->
    <div v-if="selectedCount > 0" class="batchbar" role="toolbar" aria-label="批量操作">
      <span class="batchbar-count tnum">已选 {{ selectedCount }} 项</span>
      <div class="batchbar-ops">
        <RButton size="sm" variant="ghost" :disabled="busyId !== ''" @click="runBatch('retry')">
          批量重试
        </RButton>
        <RButton size="sm" variant="ghost" :disabled="busyId !== ''" @click="runBatch('cancel')">
          批量取消
        </RButton>
        <RButton
          size="sm"
          variant="ghost"
          tone="danger"
          :disabled="busyId !== ''"
          @click="runBatch('delete')"
        >
          批量删除
        </RButton>
        <RButton size="sm" variant="ghost" @click="clearSelection">取消选择</RButton>
      </div>
    </div>

    <!-- 加载中：只在还没有任何行时显示占位文案（有行时不动列表，避免闪烁）。
         这里必须与空态互斥：§7「空态不与骨架/加载态混用」，
         两者同时出现等于告诉用户「这里什么都没有」又「正在加载」，互相矛盾。 -->
    <p v-if="loading && !roots.length" class="muted">加载中…</p>

    <!-- 截断告警独立成块（不参与下面的 v-if 链）：
         它和「有行」是同时成立的两件事——只加载了最近 100 条，但那 100 条必须照样显示。
         之前它和列表绑在同一条 v-else 链上，truncated 一旦为真，列表整个不渲染，
         用户只看见一句提示却看不到任何任务。 -->
    <p v-if="truncated" class="alert">
      任务较多，当前只加载了最近 100 条；更早的父子任务未在列表中。
    </p>

    <REmpty
      v-if="!loading && !roots.length"
      face="waiting"
      title="这个筛选下还没有任务"
      hint="粘贴链接开始第一次下载；合集或频道用「批量导入」一次建一个父子任务。"
    />

    <!-- 列表（父 → 子）。父任务用 TaskCard（子项聚合计数 + 重试失败项插槽），
         普通任务与子项用 TaskRow 紧凑行；选中态供 §7 键盘多选使用。 -->
    <div v-if="roots.length" class="list">
      <template v-for="t in roots" :key="t.id">
        <TaskCard
          v-if="isParent(t)"
          class="root-card"
          :class="{ on: selectedIds.has(t.id), 'row-flash': flashIds.has(t.id) }"
          :task="t"
          parent
          :child-stats="{
            total: statOf(t.id).total,
            failed: statOf(t.id).failed,
            done: statOf(t.id).done,
          }"
          :selected="isSelected(t.id)"
          @open="emit('open', $event)"
          @select="(task, ev) => emit('select', task, ev)"
        >
          <template #actions>
            <RButton size="sm" variant="ghost" @click.stop="emit('toggle-expand', t.id)">
              {{ isExpanded(t.id) ? '收起' : '展开子项' }}
            </RButton>
            <RButton
              size="sm"
              variant="ghost"
              tone="danger"
              :disabled="busyId === t.id"
              @click.stop="emit('remove', t)"
            >
              删除
            </RButton>
          </template>
          <template #retry-failed>
            <RButton
              size="sm"
              :disabled="busyId === t.id"
              @click.stop="emit('retry-failed-children', t)"
            >
              重试失败项
            </RButton>
          </template>
        </TaskCard>

        <TaskRow
          v-else
          :task="t"
          :class="{ 'row-flash': flashIds.has(t.id) }"
          :selected="isSelected(t.id)"
          @open="emit('open', $event)"
          @select="(task, ev) => emit('select', task, ev)"
        >
          <template #actions>
            <RButton
              v-if="isActiveStatus(t.status)"
              size="sm"
              variant="ghost"
              :disabled="busyId === t.id"
              @click.stop="emit('cancel', t.id)"
            >
              取消
            </RButton>
            <RButton
              v-if="t.status === 'failed' || t.status === 'cancelled'"
              size="sm"
              :disabled="busyId === t.id"
              @click.stop="emit('retry', t.id)"
            >
              重试
            </RButton>
            <RButton
              size="sm"
              variant="ghost"
              tone="danger"
              :disabled="busyId === t.id"
              @click.stop="emit('remove', t)"
            >
              删除
            </RButton>
          </template>
        </TaskRow>

        <!-- 子任务列表：父卡展开后逐条列出（状态点 + 迷你进度 + 标题） -->
        <ul v-if="isParent(t) && isExpanded(t.id)" class="children">
          <TaskRow
            v-for="c in shownChildren(t.id)"
            :key="c.id"
            :task="c"
            size="sm"
            :class="{ 'row-flash': flashIds.has(c.id) }"
            :selected="isSelected(c.id)"
            @open="emit('open', $event)"
            @select="(task, ev) => emit('select', task, ev)"
          >
            <template #actions>
              <RButton
                v-if="isActiveStatus(c.status)"
                size="sm"
                variant="ghost"
                :disabled="busyId === c.id"
                @click.stop="emit('cancel', c.id)"
              >
                取消
              </RButton>
              <RButton
                v-if="c.status === 'failed' || c.status === 'cancelled'"
                size="sm"
                :disabled="busyId === c.id"
                @click.stop="emit('retry', c.id)"
              >
                重试
              </RButton>
            </template>
          </TaskRow>
        </ul>
      </template>
    </div>
  </div>
</template>

<style scoped>
.filters { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 14px; }
.tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  background: var(--surface);
  color: var(--text-2);
  font-size: var(--font-body);
  cursor: pointer;
  transition: background var(--motion-micro) var(--motion-micro-ease),
    color var(--motion-micro) var(--motion-micro-ease),
    border-color var(--motion-micro) var(--motion-micro-ease);
}
.tab:hover { border-color: var(--accent); color: var(--text-1); }
.tab.on { background: var(--accent); border-color: var(--accent); color: var(--surface); }
.tab em { font-size: var(--font-aux); font-style: normal; opacity: 0.7; }

/* 批量工具条：选了东西才出现，选中态用金色左边条呼应顶栏选中态。 */
.batchbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  padding: 10px 16px;
  margin-bottom: 14px;
  background: var(--gold-soft);
  border: 1px solid var(--gold);
  border-radius: var(--radius-ctrl);
}
.batchbar-count { font-size: var(--font-body); font-weight: var(--weight-semibold); color: var(--text-1); }
.batchbar-ops { display: flex; gap: 8px; flex-wrap: wrap; }

.list { display: flex; flex-direction: column; gap: 10px; }
.root-card { margin-bottom: 0; }
.children { list-style: none; margin: 8px 0 0; padding: 0 0 0 20px; display: flex; flex-direction: column; gap: 8px; }
.muted { color: var(--text-3); font-size: var(--font-body); }
.alert {
  color: var(--warning);
  font-size: var(--font-body);
  background: var(--gold-soft);
  border: 1px solid var(--gold);
  border-radius: var(--radius-ctrl);
  padding: 10px 14px;
  margin-bottom: 14px;
}
</style>
