// 认证状态（docs/04 §2.1）：未初始化 → setup；已初始化 → login。
// 会话 cookie 为 HttpOnly，前端只感知「拿到 me / 拿到 428 / 401」。
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, setCsrfToken, getCsrfToken } from '@/api/client'
import { ApiError, type AuthUser } from '@/api/types'

export type AuthPhase = 'booting' | 'setup' | 'login' | 'ready'

export const useAuthStore = defineStore('auth', () => {
  const phase = ref<AuthPhase>('booting')
  const user = ref<AuthUser | null>(null)
  const busy = ref(false)
  const error = ref('')
  /** 会话过期/失效被登出（区别于主动登出）：登录页据此提示「为什么让我登录」。 */
  const sessionExpired = ref(false)

  /** 启动判定：先问 setup_required，再探 me（cookie 可能仍有效）。 */
  async function bootstrap(): Promise<void> {
    try {
      const st = await api.authStatus()
      if (st.setup_required) {
        phase.value = 'setup'
        return
      }
      const me = await api.authMe()
      user.value = me.user
      phase.value = 'ready'
    } catch (e) {
      if (e instanceof ApiError && (e.code === 'SETUP_REQUIRED' || e.code === 'VALIDATION')) {
        phase.value = 'setup'
        return
      }
      // 401/403：cookie 无效或过期 → 登录页
      if (e instanceof ApiError && (e.code === 'UNAUTHORIZED' || e.code === 'FORBIDDEN')) {
        phase.value = 'login'
        return
      }
      phase.value = 'login'
    }
  }

  async function setup(username: string, password: string): Promise<void> {
    busy.value = true
    error.value = ''
    try {
      const res = await api.authSetup(username, password)
      setCsrfToken(res.csrf_token)
      user.value = res.user
      phase.value = 'ready'
    } catch (e) {
      error.value = e instanceof ApiError ? e.message : '初始化失败'
      throw e
    } finally {
      busy.value = false
    }
  }

  /** login remember：true → 后端签 30 天滑动会话 + 持久化 Cookie；false → 24h + 浏览器会话 Cookie。 */
  async function login(username: string, password: string, remember = true): Promise<void> {
    busy.value = true
    error.value = ''
    try {
      const res = await api.authLogin(username, password, remember)
      setCsrfToken(res.csrf_token)
      user.value = res.user
      sessionExpired.value = false
      phase.value = 'ready'
    } catch (e) {
      error.value = e instanceof ApiError ? e.message : '登录失败'
      throw e
    } finally {
      busy.value = false
    }
  }

  /** 改用户名（PUT /auth/username）：带当前口令，成功后同步 store.user（侧栏用户名即时更新）。 */
  async function changeUsername(username: string, password: string): Promise<void> {
    busy.value = true
    error.value = ''
    try {
      const res = await api.authChangeUsername(username.trim(), password)
      user.value = res.user
    } catch (e) {
      error.value = e instanceof ApiError ? e.message : '用户名更新失败'
      throw e
    } finally {
      busy.value = false
    }
  }

  /** 改密码（PUT /auth/password）：旧口令 + 新口令；不修改会话，当前登录态保持。 */
  async function changePassword(oldPassword: string, newPassword: string): Promise<void> {
    busy.value = true
    error.value = ''
    try {
      await api.authChangePassword(oldPassword, newPassword)
    } catch (e) {
      error.value = e instanceof ApiError ? e.message : '密码更新失败'
      throw e
    } finally {
      busy.value = false
    }
  }

  async function logout(): Promise<void> {    try {
      await api.authLogout()
    } finally {
      setCsrfToken('')
      user.value = null
      phase.value = 'login'
    }
  }

  /** SSE 401 会话失效时置回登录态（不弹错误，交由外壳渲染登录页）。 */
  function markUnauthenticated(): void {
    setCsrfToken('')
    user.value = null
    sessionExpired.value = true
    phase.value = 'login'
  }

  return { phase, user, busy, error, sessionExpired, bootstrap, setup, login, logout, markUnauthenticated, changeUsername, changePassword, hasCsrf: () => !!getCsrfToken() }
})