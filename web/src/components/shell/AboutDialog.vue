<script setup lang="ts">
// AboutDialog：「关于」弹窗（2026-10-24 用户需求）——logo / slogan / GitHub 地址 / 作者。
// 从侧栏用户区打开；版本号取自 /system/info（构建注入，诚实展示 dev 也不美化）。
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import RaccoonLogo from '@/components/brand/RaccoonLogo.vue'

const open = ref(false)
const version = ref('')

const REPO_URL = 'https://github.com/huluohu/Raccoon'
const AUTHOR = 'huluohu'

onMounted(async () => {
  try {
    const res = await api.systemInfo()
    version.value = res.version
  } catch {
    version.value = '' // 拿不到就空着，不编版本号
  }
})

function openDialog() {
  open.value = true
}
function close() {
  open.value = false
}
defineExpose({ openDialog })
</script>

<template>
  <button class="about-btn" type="button" @click="openDialog">关于</button>

  <teleport to="body">
    <div v-if="open" class="about-mask" @click.self="close">
      <div class="about-card" role="dialog" aria-label="关于浣熊">
        <button class="about-close" type="button" aria-label="关闭" @click="close">×</button>
        <div class="about-brand">
          <RaccoonLogo :size="72" tagline="自托管多媒体下载中心" />
        </div>
        <p class="about-slogan">粘贴链接就能下 · 扫码即配会员源 · 数据全在本机</p>
        <dl class="about-meta">
          <div class="about-row">
            <dt>版本</dt>
            <dd class="tnum">{{ version || '—' }}</dd>
          </div>
          <div class="about-row">
            <dt>源码</dt>
            <dd>
              <a :href="REPO_URL" target="_blank" rel="noopener noreferrer">{{ REPO_URL }}</a>
            </dd>
          </div>
          <div class="about-row">
            <dt>作者</dt>
            <dd>{{ AUTHOR }}</dd>
          </div>
        </dl>
        <p class="about-note">MIT License · 不设付费激活、设备数量限制或功能墙</p>
      </div>
    </div>
  </teleport>
</template>

<style scoped>
.about-btn {
  width: 100%;
  padding: 8px 10px;
  border: none;
  border-radius: var(--radius-ctrl);
  background: transparent;
  color: var(--text-3);
  font-size: var(--font-aux);
  text-align: left;
  cursor: pointer;
}
.about-btn:hover { color: var(--text-1); background: var(--surface-2); }

.about-mask {
  position: fixed;
  inset: 0;
  z-index: 90;
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--bg) 72%, transparent);
  padding: 16px;
}
.about-card {
  position: relative;
  width: min(400px, 100%);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 24px 24px 18px;
  box-shadow: var(--shadow-3);
}
.about-close {
  position: absolute;
  top: 10px;
  right: 12px;
  border: none;
  background: none;
  color: var(--text-3);
  font-size: var(--font-title-2);
  line-height: 1;
  cursor: pointer;
  padding: 4px 8px;
}
.about-close:hover { color: var(--text-1); }
.about-brand { display: flex; justify-content: center; padding: 6px 0 2px; }
.about-slogan {
  margin: 10px 0 16px;
  text-align: center;
  font-size: var(--font-aux);
  color: var(--text-2);
}
.about-meta { margin: 0; display: flex; flex-direction: column; gap: 10px; }
.about-row { display: flex; gap: 12px; font-size: var(--font-aux); }
.about-row dt { width: 44px; flex-shrink: 0; color: var(--text-3); }
.about-row dd { margin: 0; color: var(--text-1); word-break: break-all; }
.about-row a { color: var(--accent); text-decoration: none; }
.about-row a:hover { text-decoration: underline; }
.about-note {
  margin: 16px 0 0;
  padding-top: 12px;
  border-top: 1px solid var(--border);
  text-align: center;
  font-size: var(--font-aux);
  color: var(--text-3);
}
</style>
