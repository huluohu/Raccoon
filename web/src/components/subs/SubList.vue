<script setup lang="ts">
// SubList：订阅列表（方案 §3「/subscriptions → SubList」）。
//
// 为什么抽成组件：
// 每张订阅卡要展示的信息很多（来源类型/模式徽章、标题、URL、四个计数、
// 四项元信息、错误提示、四个动作），全部堆在视图里会让 Subscriptions.vue
// 变成「一个卡片的模板 + 一点逻辑」的混合体。
// 抽出来后这张卡的结构（哪些是徽章、哪些是正文、哪些是元信息）有明确归属，
// 改卡片不会碰到页面逻辑，改页面逻辑也不会碰卡片。
import PlatformBadge from '@/components/task/PlatformBadge.vue'
import RButton from '@/components/base/RButton.vue'
import { formatRelative } from '@/utils/format'
import type { Subscription } from '@/api/types'

defineProps<{
  /** 订阅列表。 */
  items: Subscription[]
  /** 正在检查的订阅 ID（该卡的「立即检查」置灰并显示「检查中…」）。 */
  checkingId?: string
}>()

const emit = defineEmits<{
  /** 立即检查。 */
  check: [sub: Subscription]
  /** 查看已发现内容。 */
  detail: [sub: Subscription]
  /** 暂停 / 启用。 */
  toggle: [sub: Subscription]
  /** 删除。 */
  remove: [sub: Subscription]
}>()

/** 来源类型 → 中文标签。 */
const SOURCE_TYPE_TEXT: Record<string, string> = {
  favorite: '收藏夹',
  uploader: 'UP 主投稿',
  list: '播放列表',
}

/** 抓取模式 → 中文标签。 */
const MODE_TEXT: Record<string, string> = {
  new_only: '只要新增',
  backfill: '全部补齐',
  range: '指定区间',
}

/** 下次检查时间 → 人话。 */
function nextCheckText(s: Subscription): string {
  if (!s.enabled) return '已暂停'
  if (!s.next_check_at) return '待调度'
  const left = s.next_check_at - Date.now()
  if (left <= 0) return '即将检查'
  return Math.ceil(left / 60000) + ' 分钟后检查'
}

function sourceTypeText(s: Subscription): string {
  return SOURCE_TYPE_TEXT[s.source_type] || s.source_type || '自动识别'
}
</script>

<template>
  <div class="sub-list">
    <article v-for="s in items" :key="s.id" class="card" :class="{ off: !s.enabled }">
      <header class="card-head">
        <PlatformBadge :platform="s.platform" />
        <span class="src-type">{{ sourceTypeText(s) }}</span>
        <span class="mode">{{ MODE_TEXT[s.mode] || s.mode }}</span>
        <span v-if="s.error_note" class="warn-mark" :title="s.error_note">!</span>
      </header>

      <h3 class="card-title">{{ s.title || s.source_url }}</h3>
      <p class="card-url mono" :title="s.source_url">{{ s.source_url }}</p>

      <div class="counts">
        <span class="cnt done">{{ s.item_counts?.downloaded ?? 0 }} 已下载</span>
        <span class="cnt queued">{{ s.item_counts?.queued ?? 0 }} 待下载</span>
        <span v-if="(s.item_counts?.failed ?? 0) > 0" class="cnt failed">
          {{ s.item_counts?.failed }} 失败
        </span>
        <span v-if="(s.item_counts?.skipped ?? 0) > 0" class="cnt skip">
          {{ s.item_counts?.skipped }} 已跳过
        </span>
      </div>

      <dl class="meta">
        <div>
          <dt>上次检查</dt>
          <dd>{{ s.last_checked_at ? formatRelative(s.last_checked_at) : '从未' }}</dd>
        </div>
        <div><dt>下次</dt><dd>{{ nextCheckText(s) }}</dd></div>
        <div><dt>画质</dt><dd>{{ s.quality || '跟随全局' }}</dd></div>
        <div><dt>周期</dt><dd>{{ s.interval_min }} 分钟</dd></div>
      </dl>

      <p v-if="s.error_note" class="err-note">{{ s.error_note }}</p>

      <footer class="card-foot">
        <RButton
          size="sm"
          variant="accent"
          :disabled="checkingId === s.id"
          @click="emit('check', s)"
        >
          {{ checkingId === s.id ? '检查中…' : '立即检查' }}
        </RButton>
        <RButton size="sm" variant="ghost" @click="emit('detail', s)">已发现内容</RButton>
        <RButton size="sm" variant="ghost" @click="emit('toggle', s)">
          {{ s.enabled ? '暂停' : '启用' }}
        </RButton>
        <RButton
          size="sm"
          variant="ghost"
          tone="danger"
          confirm="再点一次确认删除"
          @click="emit('remove', s)"
        >
          删除
        </RButton>
      </footer>
    </article>
  </div>
</template>

<style scoped>
.sub-list { display: flex; flex-direction: column; gap: 14px; }
.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  box-shadow: var(--shadow-1);
}
/* 暂停的订阅整体降透明度——一眼看出哪些在跑。 */
.card.off { opacity: 0.66; }
.card-head { display: flex; align-items: center; gap: 8px; }
/* 来源类型 / 抓取模式：胶囊徽章（caption 档，12px 是徽章允许的下限）。 */
.src-type,
.mode {
  font-size: var(--font-caption);
  color: var(--text-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 1px 8px;
}
.warn-mark {
  margin-left: auto;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--gold-soft);
  color: var(--gold-strong);
  font-weight: var(--weight-strong);
  font-size: var(--font-caption);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--gold);
}
.card-title { font-size: var(--font-title-3); font-weight: var(--weight-medium); }
/* 订阅源 URL：辅助档 13.5px（URL 是路径原文，不是主信息）。 */
.card-url {
  font-size: var(--font-aux);
  color: var(--text-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.counts { display: flex; gap: 8px; flex-wrap: wrap; }
.cnt {
  font-size: var(--font-caption);
  padding: 1px 8px;
  border-radius: var(--radius-pill);
  background: var(--surface-2);
  color: var(--text-2);
}
.cnt.done { color: var(--success); }
.cnt.queued { color: var(--info); }
.cnt.failed { color: var(--danger); }
.cnt.skip { color: var(--text-3); }
.meta { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 6px 20px; }
/* 元信息（上次检查/下次/画质/周期）：辅助档 13.5px。 */
.meta > div { display: flex; gap: 10px; font-size: var(--font-aux); }
.meta dt { color: var(--text-3); width: 60px; flex-shrink: 0; }
.meta dd { color: var(--text-2); }
.err-note { font-size: var(--font-aux); color: var(--danger); }
.card-foot {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  padding-top: 10px;
  border-top: 1px solid var(--border);
}
</style>
