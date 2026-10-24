<script setup lang="ts">
// 登录页（方案 §3「/login | 登录 | ——」）。
//
// 首次初始化不在这里：走 /setup 的向导（components/auth/SetupWizard.vue）。
// 方案把两者列成两行，且 /login 的关键组件列是「——」——登录不需要特殊组件。
//
// 本机记忆：保存用户名（localStorage raccoon.username）与 30 天内免登录
// （后端签 30 天滑动会话 + 持久化 Cookie；不勾则 24h + 浏览器会话 Cookie）。
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'
import RaccoonLogo from '@/components/brand/RaccoonLogo.vue'

const USERNAME_KEY = 'raccoon.username'

const auth = useAuthStore()
const ui = useUiStore()
const router = useRouter()
const route = useRoute()
const username = ref('')
const password = ref('')
const saveUsername = ref(true)
const stayLoggedIn = ref(true)

onMounted(() => {
  const saved = localStorage.getItem(USERNAME_KEY)
  if (saved) username.value = saved
})

async function submit() {
  if (!username.value.trim() || !password.value) return
  try {
    await auth.login(username.value.trim(), password.value, stayLoggedIn.value)
    if (saveUsername.value) {
      localStorage.setItem(USERNAME_KEY, username.value.trim())
    } else {
      localStorage.removeItem(USERNAME_KEY)
    }
    // 认证成功：只有在 auth 路由上才需要跳走。
    // /login 的路由组件除了会话 gate 还会被外壳的 router-view 渲染一次，
    // 停在这儿不放就是「外壳 + 登录卡」同屏重复；停在别的页面（如会话过期后
    // 在 /tasks 重新登录）则原地恢复，不强行拽回总览。
    // 用 path 而不是 name：name 依赖路由表命名，反代/书签跳进来时路径才是事实。
    if (route.path === '/login' || route.path === '/setup') {
      await router.replace('/')
    }
  } catch {
    // 错误信息已在 store.error，页面直接展示
  }
}
</script>

<template>
  <section class="auth">
    <div class="card">
      <!-- 品牌区：RaccoonLogo full 变体 + 等宽眉题（控制台的第一印象：
           先看到「这是一台设备」，再看到表单）。 -->
      <div class="brand">
        <p class="brand-eyebrow eyebrow"><span class="brand-dash" aria-hidden="true" />SELF-HOSTED DOWNLOAD CENTER</p>
        <RaccoonLogo :size="56" tagline="自托管多媒体下载中心" />
      </div>

      <!-- 会话过期提示：被动登出（SSE/接口 401）时说明「为什么让我登录」，
           与主动登出区分——否则用户会误以为某个操作（如提交下载）要求登录。 -->
      <p v-if="auth.sessionExpired" class="expired-hint" role="status">
        登录状态已过期，请重新登录后继续之前的操作。
      </p>

      <form @submit.prevent="submit">
        <label>
          <span>用户名</span>
          <input v-model="username" type="text" autocomplete="username" required placeholder="admin" />
        </label>
        <label>
          <span>密码</span>
          <input v-model="password" type="password" autocomplete="current-password" required placeholder="至少 8 位" />
        </label>

        <div class="options" role="group" aria-label="登录选项">
          <label class="check">
            <input v-model="saveUsername" type="checkbox" data-test="save-username" />
            <span>保存用户名</span>
          </label>
          <label class="check">
            <input v-model="stayLoggedIn" type="checkbox" data-test="stay-logged-in" />
            <span>30 天内免登录</span>
          </label>
        </div>

        <p v-if="auth.error" class="error" role="alert">{{ auth.error }}</p>

        <button type="submit" :disabled="auth.busy">
          {{ auth.busy ? '登录中…' : '登录' }}
        </button>
      </form>
      <p class="hint">账号仅存于本机数据库，密码以 Argon2id 散列保存。</p>
      <!-- 主题切换：登录前也允许换肤（四只浣熊）；data-theme 由 App.vue 的
           watch 写到 <html>，登录页阶段同样生效。 -->
      <div class="themes" role="group" aria-label="选择主题">
        <button
          v-for="th in ui.themes"
          :key="th.id"
          type="button"
          class="theme-dot"
          :class="{ on: th.id === ui.theme }"
          :aria-pressed="th.id === ui.theme ? 'true' : 'false'"
          :title="`主题：${th.label}（${th.en}）`"
          @click="ui.setTheme(th.id)"
        >
          <span class="dot" :style="{ background: th.swatch }" aria-hidden="true" />
          <span class="theme-name">{{ th.label }}</span>
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* flex:1 + width:100%：本页在 App.vue 里是 .app（display:flex）的直接子项，
   不占满的话会被 flex 收缩成内容宽并贴主轴起点（卡片缩在左上角）。 */
.auth { flex: 1; width: 100%; display: flex; align-items: center; justify-content: center; min-height: 100vh; background: var(--bg); padding: 24px; }
.card { width: 100%; max-width: 400px; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-modal); padding: 32px; box-shadow: var(--shadow-2); }
.brand { display: flex; flex-direction: column; align-items: center; gap: 12px; margin-bottom: 24px; }
.brand-eyebrow { display: inline-flex; align-items: center; }
.brand-dash { width: 16px; height: 2px; border-radius: 1px; background: var(--accent); margin-right: 8px; }
form { display: flex; flex-direction: column; gap: 14px; }
label { display: flex; flex-direction: column; gap: 6px; font-size: var(--font-aux); color: var(--text-2); }
input { padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--bg); color: var(--text-1); font-size: var(--font-body); }
input:focus { outline: 2px solid var(--accent); outline-offset: 1px; }
/* 勾选项：横排两枚，accent-color 走围巾金（与 CTA 同语义） */
.options { display: flex; gap: 20px; flex-wrap: wrap; }
.check { flex-direction: row; align-items: center; gap: 7px; font-size: var(--font-aux); color: var(--text-2); cursor: pointer; user-select: none; }
.check input { width: 15px; height: 15px; padding: 0; margin: 0; accent-color: var(--accent); cursor: pointer; }
.check input:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
/* 登录按钮：主题强调色主 CTA（与顶栏提交按钮同一语义：本页最高频动作） */
button { margin-top: 6px; padding: 11px 16px; border: none; border-radius: var(--radius-ctrl); background: var(--cta-bg); color: var(--cta-fg); font-size: var(--font-body); font-weight: var(--weight-semibold); cursor: pointer; }
button:hover:not(:disabled) { background: var(--cta-bg-strong); }
button:disabled { opacity: 0.6; cursor: progress; }
.error { margin: 0; color: var(--danger); font-size: var(--font-aux); }
.expired-hint {
  margin: 0;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--gold-soft);
  color: var(--text-2);
  font-size: var(--font-aux);
}
.hint { margin: 18px 0 0; font-size: var(--font-caption); color: var(--text-3); }
/* 主题切换行：四枚色板圆点 + 名字，登录前的第一印象跟着主题走。 */
.themes { display: grid; grid-template-columns: repeat(2, 1fr); gap: 8px; margin-top: 16px; }
.theme-dot {
  display: inline-flex; align-items: center; justify-content: center; gap: 6px;
  padding: 6px 8px; border: 1px solid var(--border); border-radius: var(--radius-pill);
  background: transparent; color: var(--text-3); font-size: var(--font-caption);
  cursor: pointer; transition: color 120ms ease-out, border-color 120ms ease-out;
}
.theme-dot:hover { color: var(--text-1); border-color: var(--accent); }
.theme-dot.on { color: var(--accent); border-color: var(--accent); background: var(--accent-soft); }
.theme-dot .dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; }
</style>
