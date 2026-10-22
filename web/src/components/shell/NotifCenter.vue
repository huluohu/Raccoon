<script setup lang="ts">
// NotifCenter：顶栏通知按钮（未读徽章）+ 通知抽屉（docs/07 §3 通知中心）。
//
// 未读徽章的红点数字来自后端 unread 计数；点开抽屉才拉列表。已读态用「点单条」或「全部已读」。
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useNotificationsStore } from '@/stores/notifications'
import { formatRelative } from '@/utils/format'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import RButton from '@/components/base/RButton.vue'
import REmpty from '@/components/base/REmpty.vue'
import RaccoonFace from '@/components/raccoon/RaccoonFace.vue'

const notif = useNotificationsStore()
const router = useRouter()
const open = ref(false)

const KIND_TEXT: Record<string, string> = {
  task_done: '下载完成',
  task_failed: '下载失败',
  sub_new: '订阅更新',
  live_start: '直播开始',
  disk_warn: '磁盘告警',
  system: '系统',
}
const SEV_TONE: Record<string, string> = {
  info: 'var(--info)',
  success: 'var(--success)',
  warning: 'var(--warning)',
  error: 'var(--danger)',
}

/**
 * 通知类型 → 浣熊表情（方案 §5「空态/通知/加载按状态取用」）。
 *
 * 映射依据是通知的实际语义，不是 severity：
 *   task_done   → done     下载成功入库，「洗完码好了」
 *   sub_new     → surprise 追更到货，「本集比预期大 3 倍」的彩蛋感
 *   task_failed → failed   失败，「这单没成」
 *   live_start  → working  直播开录，正在忙
 *   disk_warn   → surprise 意外告警，也值得瞪大眼
 * 其余（system/info）→ working，中性忙碌。
 */
const KIND_FACE: Record<string, 'waiting' | 'working' | 'done' | 'failed' | 'cancelled' | 'surprise'> = {
  task_done: 'done',
  task_failed: 'failed',
  sub_new: 'surprise',
  live_start: 'working',
  disk_warn: 'surprise',
  system: 'working',
}

/** 取通知对应表情；未知类型回落到 waiting（平静等一等），不硬编。 */
function faceOf(kind: string) {
  return KIND_FACE[kind] ?? 'waiting'
}

async function toggle() {
  open.value = !open.value
  if (open.value) await notif.load()
}

async function markOne(id: string) {
  await notif.markRead([id])
}

async function markAll() {
  await notif.markRead([], true)
}

function goTask(taskId?: string) {
  open.value = false
  if (taskId) void router.push({ path: '/tasks', query: { id: taskId } })
}
</script>

<template>
  <div class="notif">
    <button
      class="notif-btn"
      type="button"
      :aria-label="notif.unread > 0 ? `通知，${notif.unread} 条未读` : '通知'"
      @click="toggle"
    >
      <span class="bell" aria-hidden="true">
        <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8">
          <path d="M18 8a6 6 0 1 0-12 0c0 5-2 6-2 6h16s-2-1-2-6" stroke-linecap="round" stroke-linejoin="round" />
          <path d="M10.3 20a2 2 0 0 0 3.4 0" stroke-linecap="round" />
        </svg>
      </span>
      <span v-if="notif.unread > 0" class="badge">{{ notif.unread > 99 ? '99+' : notif.unread }}</span>
    </button>

    <DetailDrawer :open="open" title="通知" :width="400" @close="open = false">
      <div class="nhead">
        <span class="ncount">{{ notif.unread }} 条未读 / 共 {{ notif.items.length }} 条</span>
        <RButton size="sm" variant="ghost" :disabled="notif.unread === 0" @click="markAll">全部已读</RButton>
      </div>

      <p v-if="notif.error" class="nerr">{{ notif.error }}</p>

      <REmpty
        v-if="!notif.loading && notif.items.length === 0"
        face="waiting"
        title="没有通知"
        hint="下载完成、失败、订阅更新、磁盘告警会出现在这里。"
      />

      <ul v-else class="nlist">
        <li
          v-for="n in notif.items"
          :key="n.id"
          class="nitem"
          :class="{ unread: !n.read }"
          @click="n.read ? goTask(n.task_id) : markOne(n.id)"
        >
          <span class="nbar" :style="{ background: SEV_TONE[n.severity] || 'var(--info)' }" aria-hidden="true" />
          <RaccoonFace :face="faceOf(n.kind)" :size="30" class="nface" />
          <div class="nbody">
            <div class="nrow">
              <span class="nkind">{{ KIND_TEXT[n.kind] || n.kind }}</span>
              <span class="ntime">{{ formatRelative(n.created_at) }}</span>
              <span v-if="!n.read" class="ndot" aria-label="未读" />
            </div>
            <p class="ntitle">{{ n.title }}</p>
            <p v-if="n.body" class="ndesc">{{ n.body }}</p>
            <button v-if="n.task_id" class="nlink" type="button" @click.stop="goTask(n.task_id)">
              跳到该任务 →
            </button>
          </div>
        </li>
      </ul>
    </DetailDrawer>
  </div>
</template>

<style scoped>
.notif { position: relative; }
.notif-btn {
  position: relative;
  width: 34px; height: 34px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--surface);
  color: var(--text-2);
  cursor: pointer;
}
.notif-btn:hover { color: var(--accent); border-color: var(--accent); }
.bell { display: inline-flex; }
.badge {
  position: absolute;
  top: -6px; right: -6px;
  min-width: 17px; height: 17px;
  padding: 0 4px;
  border-radius: var(--radius-pill);
  background: var(--danger);
  color: var(--on-danger);
  font-size: var(--font-caption);
  font-weight: var(--weight-strong);
  line-height: 17px;
  text-align: center;
}

.nhead {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding-bottom: 10px;
  margin-bottom: 8px;
  border-bottom: 1px solid var(--border);
}
.ncount { font-size: var(--font-caption); color: var(--text-3); }
.nerr { font-size: var(--font-body); color: var(--danger); margin-bottom: 8px; }

.nlist { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.nitem {
  display: flex;
  gap: 8px;
  padding: 9px 10px;
  border-radius: var(--radius-ctrl);
  background: var(--surface-2);
  cursor: pointer;
}
.nitem:hover { background: var(--surface-3); }
.nitem.unread { background: var(--accent-softer); }
.nbar { width: 3px; border-radius: 2px; flex-shrink: 0; }
/* 通知行的浣熊表情：按通知类型取表情（§5）。flex-shrink 0 保证不被压扁。 */
.nface { flex-shrink: 0; align-self: flex-start; }
.nbody { flex: 1; min-width: 0; }
.nrow { display: flex; align-items: center; gap: 6px; }
.nkind {
  font-size: var(--font-caption);
  color: var(--text-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 0 6px;
}
.ntime { font-size: var(--font-caption); color: var(--text-3); }
.ndot { margin-left: auto; width: 7px; height: 7px; border-radius: 50%; background: var(--danger); }
/* 通知标题：正文档 16px，它是这一条通知要讲的事。 */
.ntitle { margin-top: 4px; font-size: var(--font-body); font-weight: var(--weight-semibold); color: var(--text-1); }
.ndesc {
  margin-top: 4px;
  font-size: var(--font-aux);
  color: var(--text-3);
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
}
.nlink {
  margin-top: 6px;
  border: none;
  background: transparent;
  color: var(--accent);
  font-size: var(--font-aux);
  cursor: pointer;
  padding: 0;
}
.nlink:hover { text-decoration: underline; }
</style>