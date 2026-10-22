<script setup lang="ts">
// ItemsTable：订阅已发现内容表（方案 §3「/subscriptions → ItemsTable」）。
//
// 为什么抽成组件：
// 1) 表头/表体必须字号分明：表头 caption(12px)、表体 body(16px)。
//    之前整张表都是 12px，密密麻麻一坨，看不出是数据表——
//    这套字号规则要和表结构绑在一起，不能散在视图里靠继承。
// 2) 「追更到货」横幅用 surprise 表情（方案 §5），是这张表专属的情绪时刻，
//    放组件里能保证「有内容 = 有到货横幅」这条规则不被漏掉。
import RaccoonFace from '@/components/raccoon/RaccoonFace.vue'
import { formatRelative } from '@/utils/format'
import type { SubscriptionItem } from '@/api/types'

defineProps<{
  /** 已发现的条目。 */
  items: SubscriptionItem[]
}>()

/** 条目状态 → 中文。 */
const ITEM_STATUS: Record<string, string> = {
  pending: '待入队',
  queued: '已入队',
  downloaded: '已下载',
  skipped: '已跳过',
  failed: '失败',
}

/** 条目状态 → 语义色（文字本身就是颜色载体之一，不靠颜色单独承载信息）。 */
const ITEM_TONE: Record<string, string> = {
  pending: 'var(--neutral)',
  queued: 'var(--info)',
  downloaded: 'var(--success)',
  skipped: 'var(--text-3)',
  failed: 'var(--danger)',
}

/** 待处理条数（pending + queued）。 */
function pendingCount(items: SubscriptionItem[]): number {
  return items.filter((i) => i.status === 'pending' || i.status === 'queued').length
}
</script>

<template>
  <div class="found-list">
    <!-- 追更到货：方案 §5「surprise — 彩蛋、追更到货」。
         到货时用惊喜表情 + 数字事实（比预期大几倍），这是浣熊人格最该出现的时刻。 -->
    <div class="found-banner">
      <RaccoonFace face="surprise" :size="44" />
      <div>
        <p class="found-title">追更到了！</p>
        <p class="found-hint">
          本次发现 {{ items.length }} 条，其中待处理 {{ pendingCount(items) }} 条。
        </p>
      </div>
    </div>
    <table class="tbl">
      <thead>
        <tr><th>标题</th><th>状态</th><th>发现时间</th></tr>
      </thead>
      <tbody>
        <tr v-for="it in items" :key="it.id">
          <td class="ttl">{{ it.title || it.item_id }}</td>
          <td>
            <span class="st" :style="{ color: ITEM_TONE[it.status] }">
              {{ ITEM_STATUS[it.status] || it.status }}
            </span>
          </td>
          <td class="tnum">{{ formatRelative(it.discovered_at) }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.found-list { display: flex; flex-direction: column; gap: 14px; }
.found-banner {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 16px;
  border-radius: var(--radius-card);
  background: var(--gold-soft);
  border: 1px solid var(--gold);
}
.found-title {
  margin: 0;
  font-size: var(--font-title-3);
  font-weight: var(--weight-strong);
  color: var(--text-1);
}
.found-hint { margin: 4px 0 0; font-size: var(--font-body); color: var(--text-2); line-height: 1.6; }

/* 表格：表头 caption(12px)，表体 body(16px)。
   原来整张表都是 12px，表格密密麻麻一坨，完全看不出是数据表。 */
.tbl { width: 100%; border-collapse: collapse; font-size: var(--font-body); }
.tbl th {
  text-align: left;
  color: var(--text-3);
  font-weight: var(--weight-caption);
  font-size: var(--font-caption);
  padding: 8px 10px;
  border-bottom: 1px solid var(--border);
}
.tbl td { padding: 9px 10px; border-bottom: 1px solid var(--border); color: var(--text-2); }
.tbl .ttl {
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-1);
}
.tbl .tnum { white-space: nowrap; }
.st { font-weight: var(--weight-semibold); }
</style>
