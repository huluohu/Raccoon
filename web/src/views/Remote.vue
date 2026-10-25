<script setup lang="ts">
// Remote.vue：设备遥控器（对标 SXTDRemote）。
//
// 移动端专用轻量遥控面板：
// 1) 远程下发：手机直接粘贴视频链接一键下发到服务器；
// 2) 实时流监控：实时查看下载速度、进度条与任务状态；
// 3) 远程播放器：在手机上直接串流播放已下载视频或音频；
// 4) 系统状态：查看服务器磁盘余量与运行概况。
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '@/api/client'
import { useTasksStore } from '@/stores/tasks'
import { useToastStore } from '@/stores/toast'
import { formatBytes, formatDuration, formatSpeed } from '@/utils/format'
import type { MediaFile } from '@/api/types'
import RaccoonFace from '@/components/raccoon/RaccoonFace.vue'
import PlatformBadge from '@/components/task/PlatformBadge.vue'
import RButton from '@/components/base/RButton.vue'
import RSelect from '@/components/base/RSelect.vue'

const router = useRouter()
const tasks = useTasksStore()
const toast = useToastStore()

const remoteUrl = ref('')
const selectedQuality = ref('best')
const submitting = ref(false)
const recentFiles = ref<MediaFile[]>([])
const activePlayingFile = ref<MediaFile | null>(null)

const activeTasks = computed(() =>
  tasks.items.filter((t) => t.status === 'downloading' || t.status === 'parsing' || t.status === 'waiting'),
)

const totalSpeed = computed(() =>
  tasks.items
    .filter((t) => t.status === 'downloading')
    .reduce((sum, t) => sum + (t.speed_bps || 0), 0),
)

const QUALITIES = [
  { value: 'best', label: '最佳画质（推荐）' },
  { value: '1080p', label: '1080P 高清' },
  { value: '720p', label: '720P 标清' },
  { value: 'audio', label: '仅提取音频' },
]

async function submitRemoteTask() {
  const u = remoteUrl.value.trim()
  if (!u) {
    toast.error('请输入视频链接')
    return
  }
  submitting.value = true
  try {
    await tasks.submit(u, selectedQuality.value)
    toast.success('已成功远程加入下载队列')
    remoteUrl.value = ''
  } catch (e: unknown) {
    toast.error(e instanceof Error ? e.message : '远程提交失败')
  } finally {
    submitting.value = false
  }
}

async function loadRecentMedia() {
  try {
    const res = await api.listFiles({ limit: '6' })
    recentFiles.value = res.items || []
  } catch {}
}

function playMedia(f: MediaFile) {
  if (activePlayingFile.value?.id === f.id) {
    activePlayingFile.value = null
  } else {
    activePlayingFile.value = f
  }
}

onMounted(() => {
  // SSE 生命周期归 App.vue 管（全局单连接），本页不建也不断——
  // 否则离开本页时连接被关掉且无人重连，侧栏徽章会永远停在「重连中」。
  loadRecentMedia()
})
</script>

<template>
  <main class="remote-page">
    <!-- 顶栏状态条 -->
    <header class="remote-header">
      <div class="remote-brand">
        <RaccoonFace :face="activeTasks.length > 0 ? 'working' : 'done'" :size="32" />
        <span class="remote-title">Raccoon 遥控器</span>
      </div>
      <div class="remote-header-right">
        <span class="remote-live-dot" :class="{ 'is-active': tasks.connected }" />
        <span class="remote-live-status">{{ tasks.connected ? '实时连接' : '连接中' }}</span>
        <button type="button" class="remote-nav-back" @click="router.push('/')">回到控制台</button>
      </div>
    </header>

    <!-- 远程下发卡片 -->
    <section class="remote-card remote-submit-card">
      <h2 class="remote-section-title">远程下发下载</h2>
      <div class="remote-input-group">
        <input
          v-model="remoteUrl"
          class="remote-input"
          type="url"
          placeholder="长按粘贴视频链接 (哔哩哔哩/YouTube/视频号/抖音/X…)"
          @keydown.enter="submitRemoteTask"
        />
        <div class="remote-submit-opts">
          <RSelect v-model="selectedQuality" class="remote-quality-select" :options="QUALITIES" />
          <RButton
            variant="accent"
            class="remote-submit-btn"
            :disabled="submitting || !remoteUrl.trim()"
            @click="submitRemoteTask"
          >
            {{ submitting ? '下发中…' : '立即下载' }}
          </RButton>
        </div>
      </div>
    </section>

    <!-- 正在进行的任务 -->
    <section class="remote-card">
      <div class="remote-card-head">
        <h2 class="remote-section-title">下载队列 ({{ activeTasks.length }})</h2>
        <span v-if="totalSpeed > 0" class="remote-speed-pill tnum">
          {{ formatSpeed(totalSpeed) }}
        </span>
      </div>

      <div v-if="activeTasks.length === 0" class="remote-empty">
        当前无下载任务，从上方粘贴链接即可远程下发。
      </div>

      <ul v-else class="remote-task-list">
        <li v-for="t in activeTasks" :key="t.id" class="remote-task-item">
          <div class="remote-task-top">
            <PlatformBadge :platform="t.platform" />
            <span class="remote-task-name" :title="t.title || t.url">{{ t.title || t.url }}</span>
          </div>
          <div class="remote-progress-bar">
            <div class="remote-progress-fill" :style="{ width: `${Math.max(0, Math.min(100, t.progress))}%` }" />
          </div>
          <div class="remote-task-meta-hint">
            <span class="tnum">{{ t.progress > 0 ? `${t.progress.toFixed(1)}%` : '排队解析中' }}</span>
            <span v-if="t.speed_bps > 0" class="tnum">{{ formatSpeed(t.speed_bps) }}</span>
            <button type="button" class="remote-cancel-hint" @click="tasks.cancel(t.id)">取消</button>
          </div>
        </li>
      </ul>
    </section>

    <!-- 手机端直接串流播放已下载内容 -->
    <section class="remote-card">
      <h2 class="remote-section-title">已完成媒体 (点击在手机播放)</h2>
      <div v-if="activePlayingFile" class="remote-player-box">
        <video
          v-if="activePlayingFile.kind === 'video'"
          :src="api.filePreviewUrl(activePlayingFile.id)"
          controls
          autoplay
          class="remote-native-player"
        />
        <audio
          v-else-if="activePlayingFile.kind === 'audio'"
          :src="api.filePreviewUrl(activePlayingFile.id)"
          controls
          autoplay
          class="remote-native-player"
        />
        <div class="remote-player-info-hint">
          <span class="remote-playing-name">{{ activePlayingFile.name }}</span>
          <button type="button" class="remote-close-player-hint" @click="activePlayingFile = null">关闭播放</button>
        </div>
      </div>

      <ul class="remote-media-grid">
        <li
          v-for="f in recentFiles"
          :key="f.id"
          class="remote-media-item"
          :class="{ 'is-playing': activePlayingFile?.id === f.id }"
          @click="playMedia(f)"
        >
          <div class="remote-media-icon">
            <svg
              v-if="f.kind === 'video'"
              viewBox="0 0 24 24"
              width="20"
              height="20"
              fill="none"
              stroke="currentColor"
              stroke-width="1.6"
              aria-hidden="true"
            >
              <rect x="3" y="5" width="18" height="14" rx="2.5" />
              <path d="M3 9.5h18M8 5v4.5M16 5v4.5" stroke-linecap="round" />
            </svg>
            <svg
              v-else-if="f.kind === 'audio'"
              viewBox="0 0 24 24"
              width="20"
              height="20"
              fill="none"
              stroke="currentColor"
              stroke-width="1.6"
              aria-hidden="true"
            >
              <path d="M9 18.5V6.2l10-2v12.3" stroke-linecap="round" stroke-linejoin="round" />
              <circle cx="6.5" cy="18.5" r="2.5" />
              <circle cx="16.5" cy="16.5" r="2.5" />
            </svg>
            <svg
              v-else
              viewBox="0 0 24 24"
              width="20"
              height="20"
              fill="none"
              stroke="currentColor"
              stroke-width="1.6"
              aria-hidden="true"
            >
              <path d="M7 3.5h7l4.5 4.5v12a1.5 1.5 0 0 1-1.5 1.5H7a1.5 1.5 0 0 1-1.5-1.5V5a1.5 1.5 0 0 1 1.5-1.5z" stroke-linejoin="round" />
            </svg>
          </div>
          <div class="remote-media-text">
            <p class="remote-media-name">{{ f.name }}</p>
            <span class="remote-media-meta-time tnum">
              {{ formatBytes(f.size) }}
              <template v-if="f.duration">· {{ formatDuration(f.duration) }}</template>
            </span>
          </div>
        </li>
      </ul>
    </section>
  </main>
</template>

<style scoped>
.remote-page {
  max-width: 600px;
  margin: 0 auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  font-family: inherit;
  min-height: 100vh;
  box-sizing: border-box;
}

.remote-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 0;
  border-bottom: 1px solid var(--border);
}
.remote-brand {
  display: flex;
  align-items: center;
  gap: 10px;
}
.remote-title {
  font-size: var(--font-body);
  font-weight: var(--weight-bold);
  color: var(--text-1);
}
.remote-header-right {
  display: flex;
  align-items: center;
  gap: 8px;
}
.remote-live-dot {
  width: 8px;
  height: 8px;
  border-radius: var(--radius-pill);
  background: var(--warning);
}
.remote-live-dot.is-active {
  background: var(--success);
}
.remote-live-status {
  font-size: var(--font-caption);
  color: var(--text-3);
}
.remote-nav-back {
  background: none;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  padding: 4px 8px;
  font-size: var(--font-aux);
  color: var(--text-2);
  cursor: pointer;
  margin-left: 6px;
}

.remote-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  box-shadow: var(--shadow-1);
}
.remote-card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.remote-section-title {
  font-size: var(--font-aux);
  font-weight: var(--weight-semibold);
  color: var(--text-1);
  margin: 0;
}
.remote-speed-pill {
  font-size: var(--font-caption);
  font-weight: var(--weight-bold);
  background: var(--accent);
  color: var(--accent-fg);
  padding: 2px 8px;
  border-radius: var(--radius-pill);
}

.remote-input-group {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.remote-input {
  width: 100%;
  padding: 10px 14px;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  color: var(--text-1);
  font-size: var(--font-body);
  box-sizing: border-box;
  min-height: 42px;
}
.remote-input:focus {
  outline: none;
  border-color: var(--accent);
}
.remote-submit-opts {
  display: flex;
  align-items: center;
  gap: 10px;
}
.remote-quality-select {
  flex: 1;
}
.remote-submit-btn {
  height: 42px;
  padding: 0 20px;
  white-space: nowrap;
  flex-shrink: 0;
  box-sizing: border-box;
}

.remote-empty {
  font-size: var(--font-aux);
  color: var(--text-3);
  text-align: center;
  padding: 16px 0;
}

.remote-task-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.remote-task-item {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.remote-task-top {
  display: flex;
  align-items: center;
  gap: 8px;
}
.remote-task-name {
  font-size: var(--font-aux);
  color: var(--text-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}
.remote-progress-bar {
  height: 4px;
  background: var(--border);
  border-radius: var(--radius-pill);
  overflow: hidden;
}
.remote-progress-fill {
  height: 100%;
  background: var(--accent);
  transition: width 0.3s ease;
}
.remote-task-meta-hint {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: var(--font-caption);
  color: var(--text-3);
}
.remote-cancel-hint {
  background: none;
  border: none;
  color: var(--danger);
  cursor: pointer;
  padding: 0;
  font-size: var(--font-caption);
}

.remote-player-box {
  background: var(--surface-2);
  border-radius: var(--radius-ctrl);
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.remote-native-player {
  width: 100%;
  border-radius: var(--radius-sm);
  max-height: 240px;
}
.remote-player-info-hint {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: var(--font-caption);
}
.remote-playing-name {
  color: var(--text-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.remote-close-player-hint {
  background: none;
  border: none;
  color: var(--text-3);
  cursor: pointer;
  white-space: nowrap;
  font-size: var(--font-caption);
}

.remote-media-grid {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.remote-media-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px;
  border-radius: var(--radius-ctrl);
  background: var(--surface-2);
  cursor: pointer;
  border: 1px solid transparent;
  transition: border-color var(--duration-fast);
}
.remote-media-item:hover,
.remote-media-item.is-playing {
  border-color: var(--accent);
}
.remote-media-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-2);
}
.remote-media-text {
  flex: 1;
  min-width: 0;
}
.remote-media-name {
  font-size: var(--font-aux);
  color: var(--text-1);
  margin: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.remote-media-meta-time {
  font-size: var(--font-caption);
  color: var(--text-3);
}
</style>
