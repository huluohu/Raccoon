<script setup lang="ts">
// RemotePairModal：设备遥控与扫码配对弹窗（对标 SXTDRemote）。
//
// 机制说明：
// 显示当前服务器的移动端遥控器地址，支持一键复制链接，
// 用户手机浏览器或微信直接打开即可化身掌上遥控器。
import { computed, ref } from 'vue'
import RDialog from '@/components/base/RDialog.vue'
import RButton from '@/components/base/RButton.vue'
import { useToastStore } from '@/stores/toast'

defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  (e: 'update:open', val: boolean): void
}>()

const toast = useToastStore()

const remoteUrl = computed(() => {
  if (typeof window === 'undefined') return '/remote'
  return `${window.location.origin}/remote`
})

const copied = ref(false)

async function copyRemoteUrl() {
  try {
    await navigator.clipboard.writeText(remoteUrl.value)
    copied.value = true
    toast.success('遥控器链接已复制')
    setTimeout(() => {
      copied.value = false
    }, 2000)
  } catch {
    toast.error('复制失败，请手动长按复制下方链接')
  }
}
</script>

<template>
  <RDialog
    :open="open"
    title="设备配对与掌上遥控"
    width="420px"
    @update:open="emit('update:open', $event)"
  >
    <div class="pair-modal">
      <div class="pair-icon-box">
        <svg viewBox="0 0 24 24" width="40" height="40" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
          <rect x="5" y="2" width="14" height="20" rx="3" />
          <path d="M12 18h.01M9 6h6" stroke-linecap="round" />
        </svg>
      </div>

      <p class="pair-desc">
        在手机浏览器或微信中打开下方地址，即可随时随地向当前 Raccoon 下发下载、监控进度与在线播放。
      </p>

      <div class="pair-link-box">
        <code class="pair-link-path">{{ remoteUrl }}</code>
        <RButton size="sm" variant="accent" @click="copyRemoteUrl">
          {{ copied ? '已复制' : '复制链接' }}
        </RButton>
      </div>

      <div class="pair-tips-hint">
        <p>提示：同一局域网下的手机、平板均可直接访问此链接进行遥控。</p>
      </div>
    </div>
  </RDialog>
</template>

<style scoped>
.pair-modal {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 14px;
}
.pair-icon-box {
  width: 64px;
  height: 64px;
  border-radius: var(--radius-pill);
  background: color-mix(in srgb, var(--accent) 15%, transparent);
  color: var(--accent);
  display: flex;
  align-items: center;
  justify-content: center;
}
.pair-desc {
  font-size: var(--font-aux);
  color: var(--text-2);
  line-height: 1.6;
  margin: 0;
}
.pair-link-box {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  padding: 6px 10px;
  box-sizing: border-box;
}
.pair-link-path {
  flex: 1;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: var(--font-caption);
  color: var(--text-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: left;
}
.pair-tips-hint {
  font-size: var(--font-caption);
  color: var(--text-3);
  text-align: left;
  width: 100%;
}
.pair-tips-hint p {
  margin: 0;
}
</style>
