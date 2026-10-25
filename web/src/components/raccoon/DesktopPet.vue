<script setup lang="ts">
// DesktopPet：桌面常驻互动宠物（浣熊伴侣）。
//
// 特性：
// 1) 悬浮屏幕右下角，可任意拖拽位置（触摸与鼠标支持，位置持久化）；
// 2) 状态动态响应：
//    - 下载中：表情切 working，浮现当前总下载速度胶囊（如 15.2 MB/s）；
//    - 刚完成：表情切 done / surprise 开心庆祝；
//    - 异常出错：表情切 failed 呆住提醒；
//    - 空闲无任务：表情切 waiting 悠闲呼吸；
// 3) 点击交互：展开迷你快捷控制台，包含当前状态卡片、快速粘贴下载框与最小化。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useTasksStore } from '@/stores/tasks'
import { formatSpeed } from '@/utils/format'
import RaccoonFace from './RaccoonFace.vue'
import RButton from '@/components/base/RButton.vue'

const tasks = useTasksStore()

const visible = ref(true)
const openMenu = ref(false)
const quickUrl = ref('')
const submitting = ref(false)

// 拖拽位置管理
const posX = ref<number | null>(null)
const posY = ref<number | null>(null)
const isDragging = ref(false)
let dragStartX = 0
let dragStartY = 0
let initPosX = 0
let initPosY = 0

// 动态响应任务状态
const activeDownloading = computed(() =>
  tasks.items.filter((t) => t.status === 'downloading' || t.status === 'parsing'),
)
const totalSpeed = computed(() =>
  activeDownloading.value.reduce((sum, t) => sum + (t.speed_bps || 0), 0),
)
const hasFailed = computed(() => tasks.items.some((t) => t.status === 'failed'))

const currentFace = computed(() => {
  if (activeDownloading.value.length > 0) return 'working'
  if (hasFailed.value) return 'failed'
  const hasRecentDone = tasks.items.some((t) => t.status === 'done' && t.finished_at && Date.now() - t.finished_at < 30_000)
  if (hasRecentDone) return 'done'
  return 'waiting'
})

const petMoodText = computed(() => {
  if (activeDownloading.value.length > 0) {
    return `正在努力搬活（${activeDownloading.value.length} 个任务），速度 ${formatSpeed(totalSpeed.value)}`
  }
  if (hasFailed.value) {
    return '有任务遇到问题卡住了，去检查一下吧~'
  }
  return '活都搬完啦，正在悠闲打盹~'
})

function onMouseDown(e: MouseEvent) {
  // 点击弹窗内元素不触发拖拽
  if ((e.target as HTMLElement).closest('.pet-popup')) return
  isDragging.value = false
  dragStartX = e.clientX
  dragStartY = e.clientY
  const petEl = document.getElementById('desktop-pet-widget')
  if (petEl) {
    const rect = petEl.getBoundingClientRect()
    initPosX = rect.left
    initPosY = rect.top
  }
  window.addEventListener('mousemove', onMouseMove)
  window.addEventListener('mouseup', onMouseUp)
}

function onMouseMove(e: MouseEvent) {
  const dx = e.clientX - dragStartX
  const dy = e.clientY - dragStartY
  if (Math.abs(dx) > 3 || Math.abs(dy) > 3) {
    isDragging.value = true
  }
  if (isDragging.value) {
    posX.value = Math.max(10, Math.min(window.innerWidth - 80, initPosX + dx))
    posY.value = Math.max(10, Math.min(window.innerHeight - 80, initPosY + dy))
  }
}

function onMouseUp() {
  window.removeEventListener('mousemove', onMouseMove)
  window.removeEventListener('mouseup', onMouseUp)
  if (posX.value !== null && posY.value !== null) {
    try {
      localStorage.setItem('raccoon.pet_pos', JSON.stringify({ x: posX.value, y: posY.value }))
    } catch {}
  }
}

function toggleMenu() {
  if (!isDragging.value) {
    openMenu.value = !openMenu.value
  }
}

async function submitQuick() {
  const u = quickUrl.value.trim()
  if (!u) return
  submitting.value = true
  try {
    await tasks.submit(u)
    quickUrl.value = ''
    openMenu.value = false
  } catch {}
  finally {
    submitting.value = false
  }
}

onMounted(() => {
  try {
    const saved = localStorage.getItem('raccoon.pet_pos')
    if (saved) {
      const pos = JSON.parse(saved)
      if (typeof pos.x === 'number' && typeof pos.y === 'number') {
        posX.value = Math.max(10, Math.min(window.innerWidth - 80, pos.x))
        posY.value = Math.max(10, Math.min(window.innerHeight - 80, pos.y))
      }
    }
  } catch {}
})

onUnmounted(() => {
  window.removeEventListener('mousemove', onMouseMove)
  window.removeEventListener('mouseup', onMouseUp)
})
</script>

<template>
  <div
    v-if="visible"
    id="desktop-pet-widget"
    class="desktop-pet"
    :class="{ 'is-dragging': isDragging, 'has-tasks': activeDownloading.length > 0 }"
    :style="posX !== null && posY !== null ? { left: `${posX}px`, top: `${posY}px`, right: 'auto', bottom: 'auto' } : undefined"
    @mousedown="onMouseDown"
  >
    <!-- 实时下载速度小气泡 -->
    <div v-if="activeDownloading.length > 0" class="pet-speed-badge tnum" title="实时下载速度">
      {{ formatSpeed(totalSpeed) }}
    </div>

    <!-- 宠物本体头像（点击展开互动面板） -->
    <button
      type="button"
      class="pet-avatar-btn"
      :aria-label="`浣熊桌面伴侣：${petMoodText}`"
      @click="toggleMenu"
    >
      <RaccoonFace :face="currentFace" :size="52" />
    </button>

    <!-- 交互卡片展开浮层 -->
    <div v-if="openMenu" class="pet-popup" role="dialog" aria-label="浣熊控制面板">
      <div class="pet-popup-head">
        <span class="pet-popup-name">浣熊小伴侣</span>
        <button type="button" class="pet-popup-close" aria-label="收起" @click="openMenu = false">✕</button>
      </div>
      <p class="pet-popup-mood">{{ petMoodText }}</p>

      <div class="pet-popup-submit">
        <input
          v-model="quickUrl"
          class="pet-popup-input"
          type="url"
          placeholder="粘贴链接立即下载…"
          @keydown.enter="submitQuick"
        />
        <RButton size="sm" variant="accent" :disabled="submitting || !quickUrl.trim()" @click="submitQuick">
          {{ submitting ? '…' : '下载' }}
        </RButton>
      </div>

      <div class="pet-popup-foot-hint">
        <span class="pet-popup-hint">按住拖拽可改变位置</span>
        <button type="button" class="pet-popup-hide-hint" @click="visible = false">暂时收起</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.desktop-pet {
  position: fixed;
  bottom: 84px;
  right: 28px;
  z-index: 900;
  user-select: none;
  cursor: grab;
  touch-action: none;
}
.desktop-pet.is-dragging {
  cursor: grabbing;
}
.pet-avatar-btn {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: var(--shadow-2);
  transition: transform var(--duration-fast) var(--ease-snappy), box-shadow var(--duration-fast);
}
.pet-avatar-btn:hover {
  transform: scale(1.08);
  box-shadow: var(--shadow-3);
}
.desktop-pet.has-tasks .pet-avatar-btn {
  border-color: color-mix(in srgb, var(--accent) 50%, var(--border));
  box-shadow: 0 0 12px color-mix(in srgb, var(--accent) 30%, transparent);
}

.pet-speed-badge {
  position: absolute;
  top: -8px;
  right: -6px;
  background: var(--accent);
  color: var(--accent-fg);
  font-size: var(--font-caption);
  font-weight: var(--weight-bold);
  padding: 2px 7px;
  border-radius: var(--radius-pill);
  box-shadow: var(--shadow-1);
  white-space: nowrap;
}

.pet-popup {
  position: absolute;
  bottom: 68px;
  right: 0;
  width: 280px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 14px;
  box-shadow: var(--shadow-3);
  display: flex;
  flex-direction: column;
  gap: 10px;
  cursor: default;
}
.pet-popup-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.pet-popup-name {
  font-size: var(--font-aux);
  font-weight: var(--weight-semibold);
  color: var(--text-1);
}
.pet-popup-close {
  background: none;
  border: none;
  color: var(--text-3);
  font-size: var(--font-aux);
  cursor: pointer;
  padding: 2px 6px;
}
.pet-popup-close:hover {
  color: var(--text-1);
}
.pet-popup-mood {
  font-size: var(--font-aux);
  color: var(--text-2);
  line-height: 1.5;
  margin: 0;
}
.pet-popup-submit {
  display: flex;
  gap: 6px;
}
.pet-popup-input {
  flex: 1;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  padding: 6px 10px;
  font-size: var(--font-aux);
  color: var(--text-1);
  min-width: 0;
}
.pet-popup-input:focus {
  outline: none;
  border-color: var(--accent);
}
.pet-popup-foot-hint {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: var(--font-caption);
  color: var(--text-3);
}
.pet-popup-hide-hint {
  background: none;
  border: none;
  color: var(--text-3);
  text-decoration: underline;
  cursor: pointer;
  font-size: var(--font-caption);
}
.pet-popup-hide-hint:hover {
  color: var(--danger);
}
</style>
