<script setup lang="ts">
// 登录 / 首次初始化（后端 428 setup_required 语义的前端落点）。
import { ref } from 'vue'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const username = ref('')
const password = ref('')
const confirm = ref('')

const isSetup = () => auth.phase === 'setup'

async function submit() {
  if (!username.value.trim() || !password.value) return
  if (isSetup() && password.value !== confirm.value) {
    auth.error = '两次输入的密码不一致'
    return
  }
  try {
    if (isSetup()) await auth.setup(username.value.trim(), password.value)
    else await auth.login(username.value.trim(), password.value)
  } catch {
    // 错误信息已在 store.error，页面直接展示
  }
}
</script>

<template>
  <section class="auth">
    <div class="card">
      <div class="brand">
        <span class="logo" aria-hidden="true">🦝</span>
        <div>
          <h1>浣熊 <em>Raccoon</em></h1>
          <p class="sub">{{ isSetup() ? '首次使用：创建管理员账号' : '请登录以继续' }}</p>
        </div>
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
        <label v-if="isSetup()">
          <span>确认密码</span>
          <input v-model="confirm" type="password" autocomplete="new-password" required placeholder="再输入一次" />
        </label>

        <p v-if="auth.error" class="error">{{ auth.error }}</p>

        <button type="submit" :disabled="auth.busy">
          {{ auth.busy ? '处理中…' : isSetup() ? '创建账号并进入' : '登录' }}
        </button>
      </form>
      <p class="hint">账号仅存于本机数据库，密码以 Argon2id 散列保存。</p>
    </div>
  </section>
</template>

<style scoped>
.auth { display: flex; align-items: center; justify-content: center; min-height: 100vh; background: var(--bg); padding: 24px; }
.card { width: 100%; max-width: 400px; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-modal); padding: 32px; box-shadow: var(--shadow-2); }
.brand { display: flex; gap: 12px; align-items: center; margin-bottom: 24px; }
.logo { font-size: 34px; }
h1 { margin: 0; font-size: 22px; font-weight: 750; }
h1 em { font-style: normal; font-size: 13px; color: var(--text-3); margin-left: 4px; }
.sub { margin: 4px 0 0; font-size: var(--font-aux); color: var(--text-2); }
form { display: flex; flex-direction: column; gap: 14px; }
label { display: flex; flex-direction: column; gap: 6px; font-size: var(--font-aux); color: var(--text-2); }
input { padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--bg); color: var(--text-1); font-size: var(--font-body); }
input:focus { outline: 2px solid var(--gold); outline-offset: 1px; }
button { margin-top: 6px; padding: 11px 16px; border: none; border-radius: var(--radius-ctrl); background: var(--accent); color: #fff; font-size: var(--font-body); font-weight: 600; cursor: pointer; }
button:disabled { opacity: 0.6; cursor: progress; }
.error { margin: 0; color: var(--danger); font-size: var(--font-aux); }
.hint { margin: 18px 0 0; font-size: var(--font-caption); color: var(--text-3); }
</style>