<script setup lang="ts">
// BiliQrLogin：B 站扫码登录（免手动 Cookie 的凭据获取通道）。
//
// 为什么值得一个独立组件：扫码是一个有生命周期的状态机
//（取码 → 等扫码 → 等确认 → 成功/过期），轮询节奏、过期重取、
// 成功后的凭据刷新都藏在组件里，连接页只管开门和收「done」事件。
// 二维码在前端渲染（qrcode 库）：后端只回 B 站给的登录 URL，不中转图片。
import { onBeforeUnmount, ref } from 'vue'
import QRCode from 'qrcode'
import { api } from '@/api/client'
import { useToastStore } from '@/stores/toast'
import RButton from '@/components/base/RButton.vue'

const toast = useToastStore()

const emit = defineEmits<{ (e: 'done'): void }>()

const open = ref(false)
const starting = ref(false)
/** 二维码图片 dataURL（qrcode 库离线渲染，不经任何第三方图床）。 */
const qrDataUrl = ref('')
/** 轮询状态机：idle → waiting_scan → scanned → success | expired。 */
const phase = ref<'idle' | 'waiting_scan' | 'scanned' | 'success' | 'expired'>('idle')
const message = ref('')
let qrcodeKey = ''
let pollTimer: ReturnType<typeof setTimeout> | null = null
/** 轮询代数：重取二维码后旧轮询不得再写状态。 */
let pollGeneration = 0

function stopPolling() {
  if (pollTimer !== null) {
    clearTimeout(pollTimer)
    pollTimer = null
  }
}

/** 读主题 Token（二维码纸墨色；jsdom 下拿不到时交给 qrcode 库默认黑白）。 */
function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

onBeforeUnmount(stopPolling)

async function start() {
  open.value = true
  await refreshQR()
}

/** 取新二维码并开始轮询（首次进入与过期重取共用）。 */
async function refreshQR() {
  starting.value = true
  phase.value = 'idle'
  message.value = ''
  qrDataUrl.value = ''
  stopPolling()
  pollGeneration++
  const gen = pollGeneration
  try {
    const res = await api.biliQrStart()
    qrcodeKey = res.data.qrcode_key
    const ink = cssVar('--qr-ink')
    const paper = cssVar('--qr-paper')
    qrDataUrl.value = await QRCode.toDataURL(res.data.url, {
      width: 264,
      margin: 1,
      ...(ink && paper ? { color: { dark: ink, light: paper } } : {}),
    })
    phase.value = 'waiting_scan'
    schedulePoll(gen)
  } catch (e) {
    message.value = e instanceof Error ? e.message : '二维码获取失败'
  } finally {
    starting.value = false
  }
}

/** 2s 轮询；每轮带代数校验，防旧会话的迟到响应覆盖新会话状态。 */
function schedulePoll(gen: number) {
  pollTimer = setTimeout(async () => {
    if (gen !== pollGeneration) return
    try {
      const res = await api.biliQrPoll(qrcodeKey)
      if (gen !== pollGeneration) return
      const st = res.data.status
      if (st === 'success') {
        phase.value = 'success'
        message.value = '登录成功，凭据已加密保存'
        toast.success('B 站登录态已入凭据中心')
        emit('done')
        return
      }
      if (st === 'expired') {
        phase.value = 'expired'
        message.value = '二维码已过期'
        return
      }
      if (st === 'scanned') phase.value = 'scanned'
      else phase.value = 'waiting_scan'
      schedulePoll(gen)
    } catch (e) {
      if (gen !== pollGeneration) return
      // 单次网络抖动不判死：继续按节奏轮询，连续失败由用户关闭弹窗终结。
      message.value = e instanceof Error ? e.message : ''
      schedulePoll(gen)
    }
  }, 2000)
}

function close() {
  stopPolling()
  pollGeneration++
  open.value = false
}

const phaseText: Record<typeof phase.value, string> = {
  idle: '正在获取二维码…',
  waiting_scan: '打开 B 站 App 扫码（扫一扫 → 右下角相册旁的扫图标）',
  scanned: '已扫码，请在手机上点「确认登录」',
  success: '登录成功',
  expired: '二维码已过期',
}
</script>

<template>
  <RButton variant="ghost" :disabled="starting" @click="start">
    {{ starting ? '取码中…' : 'B站扫码登录' }}
  </RButton>

  <teleport to="body">
    <div v-if="open" class="qr-mask" @click.self="close">
      <div class="qr-card" role="dialog" aria-label="B 站扫码登录">
        <header class="qr-head">
          <h3 class="qr-title">B 站扫码登录</h3>
          <button class="qr-close" type="button" aria-label="关闭" @click="close">×</button>
        </header>
        <p class="qr-sub">
          用手机 B 站 App 扫码确认后，登录态直接加密进凭据中心——
          不用再去浏览器里抄 Cookie。会员视频需要账号本身有会员权限。
        </p>
        <div class="qr-stage">
          <img v-if="qrDataUrl" class="qr-img" :src="qrDataUrl" alt="B 站登录二维码" />
          <div v-else class="qr-placeholder">生成中…</div>
          <div v-if="phase === 'scanned'" class="qr-badge qr-badge--scanned">已扫码</div>
          <div v-else-if="phase === 'success'" class="qr-badge qr-badge--success">✓ 已登录</div>
          <div v-else-if="phase === 'expired'" class="qr-badge qr-badge--expired">已过期</div>
        </div>
        <p class="qr-status" :class="{ 'qr-status--success': phase === 'success' }">
          {{ message || phaseText[phase] }}
        </p>
        <div class="qr-actions">
          <RButton v-if="phase === 'expired'" variant="accent" @click="refreshQR">重新取码</RButton>
          <RButton v-if="phase === 'success'" variant="accent" @click="close">完成</RButton>
        </div>
      </div>
    </div>
  </teleport>
</template>

<style scoped>
/* 遮罩弹层（复用全局 dialog 节奏：深底遮罩 + 居中卡片）。 */
.qr-mask {
  position: fixed;
  inset: 0;
  z-index: 90;
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--bg) 72%, transparent);
  padding: 16px;
}
.qr-card {
  width: min(360px, 100%);
  background: var(--surface-1);
  border: 1px solid var(--border);
  border-radius: var(--radius-card, 14px);
  padding: 18px 18px 16px;
  box-shadow: var(--shadow-3);
}
.qr-head { display: flex; align-items: center; justify-content: space-between; }
.qr-title { margin: 0; font-size: var(--font-title-2); font-weight: var(--weight-strong); }
.qr-close {
  border: none; background: none; color: var(--text-3);
  font-size: var(--font-title-2); line-height: 1; cursor: pointer; padding: 4px 8px;
}
.qr-close:hover { color: var(--text-1); }
.qr-sub { margin: 8px 0 14px; font-size: var(--font-aux); color: var(--text-3); line-height: 1.7; }
/* 二维码必须纸白底深墨点：扫码器对深底透出的码识别率不可靠（Token --qr-paper/ink）。 */
.qr-stage { position: relative; display: flex; justify-content: center; padding: 12px; background: var(--qr-paper); border-radius: 10px; }
.qr-img { display: block; width: 264px; height: 264px; }
.qr-placeholder { width: 264px; height: 264px; display: flex; align-items: center; justify-content: center; color: var(--text-3); font-size: var(--font-aux); }
.qr-badge {
  position: absolute; top: 10px; right: 10px;
  padding: 4px 10px; border-radius: 999px;
  font-size: var(--font-caption); font-weight: var(--weight-semibold);
}
.qr-badge--scanned { background: var(--gold-soft); color: var(--text-1); }
.qr-badge--success { background: color-mix(in srgb, var(--success) 18%, transparent); color: var(--success); }
.qr-badge--expired { background: color-mix(in srgb, var(--danger) 18%, transparent); color: var(--danger); }
.qr-status { margin: 12px 0 4px; font-size: var(--font-body); color: var(--text-2); text-align: center; }
.qr-status--success { color: var(--success); font-weight: var(--weight-semibold); }
.qr-actions { display: flex; justify-content: center; gap: 10px; margin-top: 8px; }
</style>
