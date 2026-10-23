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
import RaccoonLogo from '@/components/brand/RaccoonLogo.vue'

const USERNAME_KEY = 'raccoon.username'

const auth = useAuthStore()
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
      <!-- 品牌区：RaccoonLogo full 变体（徽标 + 字标 + 一句话定位）。
           登录页是品牌第一印象，用会变表情的 RaccoonFace 撑不起门面。 -->
      <div class="brand">
        <RaccoonLogo :size="56" tagline="自托管多媒体下载中心" />
      </div>

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
    </div>
  </section>
</template>

<style scoped>
/* flex:1 + width:100%：本页在 App.vue 里是 .app（display:flex）的直接子项，
   不占满的话会被 flex 收缩成内容宽并贴主轴起点（卡片缩在左上角）。 */
.auth { flex: 1; width: 100%; display: flex; align-items: center; justify-content: center; min-height: 100vh; background: var(--bg); padding: 24px; }
.card { width: 100%; max-width: 400px; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-modal); padding: 32px; box-shadow: var(--shadow-2); }
.brand { display: flex; justify-content: center; margin-bottom: 24px; }
form { display: flex; flex-direction: column; gap: 14px; }
label { display: flex; flex-direction: column; gap: 6px; font-size: var(--font-aux); color: var(--text-2); }
input { padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--bg); color: var(--text-1); font-size: var(--font-body); }
input:focus { outline: 2px solid var(--gold); outline-offset: 1px; }
/* 勾选项：横排两枚，accent-color 走围巾金（与 CTA 同语义） */
.options { display: flex; gap: 20px; flex-wrap: wrap; }
.check { flex-direction: row; align-items: center; gap: 7px; font-size: var(--font-aux); color: var(--text-2); cursor: pointer; user-select: none; }
.check input { width: 15px; height: 15px; padding: 0; margin: 0; accent-color: var(--gold); cursor: pointer; }
.check input:focus-visible { outline: 2px solid var(--gold); outline-offset: 1px; }
/* 登录按钮：围巾金主 CTA（与顶栏提交按钮同一语义：本页最高频动作） */
button { margin-top: 6px; padding: 11px 16px; border: none; border-radius: var(--radius-ctrl); background: var(--cta-bg); color: var(--cta-fg); font-size: var(--font-body); font-weight: var(--weight-semibold); cursor: pointer; }
button:hover:not(:disabled) { background: var(--cta-bg-strong); }
button:disabled { opacity: 0.6; cursor: progress; }
.error { margin: 0; color: var(--danger); font-size: var(--font-aux); }
.hint { margin: 18px 0 0; font-size: var(--font-caption); color: var(--text-3); }
</style>
