<script setup lang="ts">
// SetupWizard：首次初始化的向导表单（方案 §3「/setup | 初始化管理员 | 向导表单」）。
//
// 为什么独立成向导而不是复用登录表单：
// 1) 两件事的语义不同。登录是「回到一个已存在的系统」，初始化是「把这个系统变成你的」——
//    后者要交代清楚「接下来会发生什么」「以后去哪改」，前者一句提示就够了。
// 2) 方案把 /setup 和 /login 分成两行，且 /login 的关键组件列是「——」。
//    让一个组件兼两职，等于 /setup 那一行是空的。
// 3) 引导能降低首次使用门槛：初始化是唯一一次「从零开始」的时刻，
//    错过了就只能靠记忆找回入口（设置页 → 账号）。
//
// 步骤设计（两步）：
//   ① 账号信息：用户名 + 口令 + 确认口令（当场校验，不发请求）
//   ② 确认与说明：复核将要创建的账号，并说明后续在哪里改
// 两步都不发请求，直到最后一步才创建——避免用户填错一遍还要重填。
//
// 铁律：创建成功后由 auth store 切到 ready，外壳自动渲染主界面；
// 本组件不自己跳路由（会话 gate 已经接管，重复跳转会闪一下）。
import { computed, ref } from 'vue'
import { useAuthStore } from '@/stores/auth'
import RaccoonLogo from '@/components/brand/RaccoonLogo.vue'

const auth = useAuthStore()

/** 当前步骤：1 = 填账号，2 = 复核确认。 */
const step = ref<1 | 2>(1)
const username = ref('')
const password = ref('')
const confirm = ref('')

/** 表单级错误（当场校验，不打扰后端）。 */
const errorMsg = ref('')

/** 用户名规则与后端一致（2–32 位，字母/数字/_/-/中文）。 */
const USERNAME_RE = /^[a-zA-Z0-9_\-\u3400-\u4DBF\u4E00-\u9FFF\uF900-\uFAFF]+$/

/** 第一步能否进入下一步。 */
const step1Valid = computed(() => {
  const name = username.value.trim()
  if (name.length < 2 || name.length > 32) return false
  if (!USERNAME_RE.test(name)) return false
  if (password.value.length < 8) return false
  return password.value === confirm.value
})

/** 第一步的即时提示（告诉用户差什么，而不是只给一个不能点的按钮）。 */
const step1Hint = computed(() => {
  const name = username.value.trim()
  if (name && (name.length < 2 || name.length > 32)) return '用户名长度需在 2–32 之间'
  if (name && !USERNAME_RE.test(name)) return '用户名仅限字母/数字/_/-/中文'
  if (password.value && password.value.length < 8) return '口令至少 8 位'
  if (confirm.value && password.value !== confirm.value) return '两次输入的口令不一致'
  return ''
})

function next() {
  errorMsg.value = ''
  if (!step1Valid.value) {
    errorMsg.value = step1Hint.value || '请把账号信息填完整'
    return
  }
  step.value = 2
}

function back() {
  errorMsg.value = ''
  step.value = 1
}

async function create() {
  errorMsg.value = ''
  auth.busy = true
  try {
    await auth.setup(username.value.trim(), password.value)
    // 成功后 auth.phase 变 'ready'，外壳自动切换到主界面，这里不再跳路由。
  } catch (e) {
    // 直接从异常对象取原因，不依赖 store.error 的透传：
    // 后端文案（用户名已存在 / 口令不合策略）是最有用的信息，
    // 兜底才用 store.error，最后才用通用文案。
    errorMsg.value =
      (e instanceof Error && e.message) || auth.error || '初始化失败'
    // 创建失败 → 回到第一步改，别把人卡在确认页上。
    step.value = 1
  } finally {
    auth.busy = false
  }
}
</script>

<template>
  <section class="wizard">
    <div class="card">
      <div class="brand">
        <RaccoonLogo :size="56" tagline="首次使用 · 初始化管理员" />
      </div>

      <!-- 步骤指示：两个点 + 当前态，不靠文字颜色单独承载信息 -->
      <ol class="steps" aria-label="初始化步骤">
        <li class="step" :class="{ on: step === 1, done: step > 1 }">① 账号信息</li>
        <li class="step" :class="{ on: step === 2 }">② 确认创建</li>
      </ol>

      <form v-if="step === 1" @submit.prevent="next">
        <label>
          <span>用户名</span>
          <input
            v-model="username"
            type="text"
            autocomplete="username"
            required
            placeholder="admin"
            aria-describedby="wizard-hint"
          />
        </label>
        <label>
          <span>口令</span>
          <input
            v-model="password"
            type="password"
            autocomplete="new-password"
            required
            placeholder="至少 8 位"
          />
        </label>
        <label>
          <span>确认口令</span>
          <input
            v-model="confirm"
            type="password"
            autocomplete="new-password"
            required
            placeholder="再输入一次"
          />
        </label>

        <p v-if="step1Hint" id="wizard-hint" class="hint">{{ step1Hint }}</p>

        <!-- 创建失败回到第 1 步时，错误原因必须还在这儿（否则用户只看到表单突然变了样） -->
        <p v-if="errorMsg" class="error" role="alert">{{ errorMsg }}</p>

        <button type="submit" :disabled="!step1Valid || auth.busy">下一步</button>
      </form>

      <form v-else @submit.prevent="create">
        <dl class="review">
          <div class="review-row">
            <dt>用户名</dt>
            <dd class="tnum">{{ username.trim() }}</dd>
          </div>
          <div class="review-row">
            <dt>口令</dt>
            <dd>已设置（Argon2id 散列存储，不可查看）</dd>
          </div>
        </dl>

        <ul class="notes">
          <li>创建后立即进入浣熊，无需再登录一次。</li>
          <li>本机单管理员账号，以后可在「设置 → 账号」里改用户名或口令。</li>
          <li>账号只存在这台机器的数据库里，口令不会以明文形式落盘。</li>
        </ul>

        <p v-if="errorMsg" class="error" role="alert">{{ errorMsg }}</p>

        <div class="actions">
          <button type="button" class="ghost" @click="back">上一步</button>
          <button type="submit" :disabled="auth.busy">
            {{ auth.busy ? '创建中…' : '创建账号并进入' }}
          </button>
        </div>
      </form>
    </div>
  </section>
</template>

<style scoped>
.wizard { flex: 1; width: 100%; display: flex; align-items: center; justify-content: center; min-height: 100vh; background: var(--bg); padding: 24px; }
.card { width: 100%; max-width: 460px; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-modal); padding: 32px; box-shadow: var(--shadow-2); }
.brand { display: flex; justify-content: center; margin-bottom: 20px; }
/* 步骤指示：当前步用色块 + 文字，色弱用户也能靠位置读出进度。 */
.steps { list-style: none; display: flex; gap: 8px; margin: 0 0 20px; padding: 0; }
.step {
  flex: 1; padding: 6px 10px; border-radius: var(--radius-ctrl);
  background: var(--surface-2); color: var(--text-3);
  font-size: var(--font-aux); text-align: center;
}
.step.on { background: var(--gold-soft); color: var(--text-1); font-weight: var(--weight-semibold); }
.step.done { color: var(--success); }
form { display: flex; flex-direction: column; gap: 14px; }
label { display: flex; flex-direction: column; gap: 6px; font-size: var(--font-aux); color: var(--text-2); }
input { padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--bg); color: var(--text-1); font-size: var(--font-body); }
input:focus { outline: 2px solid var(--gold); outline-offset: 1px; }
button { margin-top: 6px; padding: 11px 16px; border: none; border-radius: var(--radius-ctrl); background: var(--accent); color: var(--on-accent); font-size: var(--font-body); font-weight: var(--weight-semibold); cursor: pointer; }
button:disabled { opacity: 0.6; cursor: progress; }
button.ghost { background: var(--surface-2); color: var(--text-1); border: 1px solid var(--border); }
.actions { display: flex; gap: 10px; }
.actions .ghost { flex: 0 0 auto; margin-top: 6px; }
.actions button[type='submit'] { flex: 1; }
.review { margin: 0; display: flex; flex-direction: column; gap: 8px; }
.review-row { display: flex; gap: 12px; align-items: baseline; }
.review dt { font-size: var(--font-aux); color: var(--text-3); min-width: 60px; }
.review dd { margin: 0; font-size: var(--font-body); color: var(--text-1); }
.notes { margin: 0; padding-left: 20px; display: flex; flex-direction: column; gap: 6px; }
.notes li { font-size: var(--font-aux); color: var(--text-2); line-height: 1.6; }
.hint { margin: 0; font-size: var(--font-aux); color: var(--text-3); }
.error { margin: 0; color: var(--danger); font-size: var(--font-aux); }
</style>
