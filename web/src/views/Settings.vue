<script setup lang="ts">
// 设置页（docs/07 §3 SettingItem / docs/04 §2.4）：
//
// 五要素（说明/默认值/校验/敏感/生效时机）+ 生效时机徽章 + env 锁定只读 +
// 脏状态拦截（路由离开提示）+ 测试连接 + 历史回滚 + API Token 管理。
//
// 铁律：保存结果按接口返回的 effective_summary 展示，不在前端猜生效时机。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { api } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useToastStore } from '@/stores/toast'
import { formatRelative } from '@/utils/format'
import type { ApiToken, ConfigHistoryEntry, ConfigItem, Effective } from '@/api/types'
import RButton from '@/components/base/RButton.vue'
import RInput from '@/components/base/RInput.vue'
import RSelect from '@/components/base/RSelect.vue'
import REmpty from '@/components/base/REmpty.vue'
import SettingsNav from '@/components/settings/SettingsNav.vue'
import SettingItem from '@/components/settings/SettingItem.vue'
import DiagPanel from '@/components/settings/DiagPanel.vue'
import type { SettingsSection } from '@/components/settings/SettingsNav.vue'

const toast = useToastStore()
const auth = useAuthStore()

const items = ref<ConfigItem[]>([])
const loading = ref(true)
const errorMsg = ref('')
const saving = ref(false)
const drafts = ref<Record<string, unknown>>({})
const dirtyKeys = ref<Set<string>>(new Set())
const history = ref<ConfigHistoryEntry[]>([])
const historyKey = ref('')
const tokens = ref<ApiToken[]>([])
const newTokenName = ref('')
const newTokenScope = ref(3)
const newTokenPlain = ref('')
const testing = ref(false)

/** 分组（按 key 前缀）。 */
const GROUPS: { key: string; label: string }[] = [
  { key: 'storage', label: '存储' },
  { key: 'download', label: '下载' },
  { key: 'net', label: '网络' },
  { key: 'subscription', label: '订阅' },
  { key: 'live', label: '直播' },
  { key: 'notify', label: '通知' },
  { key: 'log', label: '日志' },
  { key: 'server', label: '服务' },
  { key: 'qb', label: 'qBittorrent' },
  { key: 'n115', label: '115 网盘' },
  { key: 'telegram', label: 'Telegram' },
]

const grouped = computed(() =>
  GROUPS.map((g) => ({
    ...g,
    items: items.value.filter((i) => i.key.startsWith(g.key + '.') || i.key === g.key),
  })).filter((g) => g.items.length > 0),
)

/**
 * 左分组导航的分组清单（方案 §3「SettingsNav（左分组）」）。
 *
 * 顺序 = 导航项顺序 = 文档顺序（账号 → 各配置分组 → 通道测试 → 令牌 → 历史）。
 * scrollspy 的判定线、底部兜底、点击锁定三机制在 SettingsNav 组件内部实现，
 * 这里只负责把「有哪些分组」如实告诉它。
 */
const navSections = computed<SettingsSection[]>(() => [
  { id: 'account', label: '账号' },
  ...grouped.value.map((g) => ({ id: g.key, label: g.label })),
  { id: 'conn', label: '通道测试' },
  { id: 'diag', label: '诊断' },
  { id: 'tokens', label: 'API 令牌' },
  { id: 'history', label: '配置历史' },
])

const isDirty = computed(() => dirtyKeys.value.size > 0)

/** 生效时机徽章文案（与 SettingItem 保持同一套说法）。 */
const EFFECTIVE_TEXT: Record<Effective, string> = {
  immediate: '立即生效',
  next_task: '下个任务生效',
  restart: '需重启生效',
}

/**
 * 逐项保存反馈（方案 §3「SettingItem（生效时机徽章 + 脏圆点 + 保存反馈）」）。
 *
 * key → 保存后这一项的生效时机文案。
 * 数据来源只有后端返回的 effective_summary（铁律：前端不猜生效时机），
 * 接口没提到的 key 就没反馈，不编。
 */
const savedHints = ref<Record<string, string>>({})

/** 反馈自动消失的定时器：反馈是「刚刚发生了什么」，不是永久状态。 */
let hintTimer: ReturnType<typeof setTimeout> | null = null

/** 保存成功后把 effective_summary 摊平成 key → 文案。 */
function buildSavedHints(summary: Record<Effective, string[]>): Record<string, string> {
  const out: Record<string, string> = {}
  for (const eff of Object.keys(EFFECTIVE_TEXT) as Effective[]) {
    for (const key of summary[eff] ?? []) {
      out[key] = `已保存 · ${EFFECTIVE_TEXT[eff]}`
    }
  }
  return out
}

/** 展示逐项保存反馈，并在若干秒后自动收回。 */
function showSavedHints(summary: Record<Effective, string[]>): void {
  savedHints.value = buildSavedHints(summary)
  if (hintTimer) clearTimeout(hintTimer)
  hintTimer = setTimeout(() => {
    savedHints.value = {}
    hintTimer = null
  }, 6000)
}

function draftOf(item: ConfigItem): unknown {
  if (item.key in drafts.value) return drafts.value[item.key]
  // 敏感项不回填掩码：掩码不是真实值，回填后保存会把掩码写进配置。
  if (item.sensitive) return ''
  return item.value
}

function setDraft(item: ConfigItem, val: unknown) {
  drafts.value = { ...drafts.value, [item.key]: val }
  const next = new Set(dirtyKeys.value)
  if (item.sensitive) {
    // 敏感项：留空 = 不修改（与凭据中心同一语义）。
    if (typeof val === 'string' && val !== '') next.add(item.key)
    else next.delete(item.key)
  } else if (JSON.stringify(val) !== JSON.stringify(item.value)) {
    next.add(item.key)
  } else {
    next.delete(item.key)
  }
  dirtyKeys.value = next
  // 改动即作废该项的旧「已保存」反馈：它已经不是当前值了，继续显示会骗人。
  if (savedHints.value[item.key]) {
    const hints = { ...savedHints.value }
    delete hints[item.key]
    savedHints.value = hints
  }
}

async function load() {
  loading.value = true
  errorMsg.value = ''
  try {
    const res = await api.getConfig()
    items.value = res.items ?? []
    drafts.value = {}
    dirtyKeys.value = new Set()
    await Promise.all([loadHistory(), loadTokens()])
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : '配置加载失败'
  } finally {
    loading.value = false
  }
}

async function loadHistory() {
  try {
    const res = await api.getConfigHistory(historyKey.value || undefined)
    history.value = res.items ?? []
  } catch {
    history.value = []
  }
}

async function loadTokens() {
  try {
    const res = await api.listTokens()
    tokens.value = res.items ?? []
  } catch {
    tokens.value = []
  }
}

async function save() {
  if (!isDirty.value) return
  saving.value = true
  const payload: Record<string, unknown> = {}
  for (const k of dirtyKeys.value) payload[k] = drafts.value[k]
  try {
    const res = await api.saveConfig(payload)
    const s = res.effective_summary
    const parts: string[] = []
    if (s.immediate?.length) parts.push(`立即生效：${s.immediate.join('、')}`)
    if (s.next_task?.length) parts.push(`下个任务生效：${s.next_task.join('、')}`)
    if (s.restart?.length) parts.push(`需重启生效：${s.restart.join('、')}`)
    toast.success(parts.length ? '已保存 · ' + parts.join('；') : '已保存')
    // 逐项反馈：哪一项什么时候生效，直接标在这一项上（§3 SettingItem）。
    showSavedHints(s)
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}

async function rollback(entry: ConfigHistoryEntry) {
  try {
    await api.rollbackConfig(entry.key, entry.version)
    toast.success(`已回滚 ${entry.key} 到版本 ${entry.version}（写入为新版本）`)
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '回滚失败')
  }
}

/** 通道测试结果（用服务端已保存的配置实探，不模拟、不假装成功）。 */
const channelResult = ref<Record<string, { ok: boolean; message: string }>>({})

async function testChannel(key: 'qb' | 'n115' | 'telegram') {
  testing.value = true
  try {
    if (key === 'telegram') {
      // Telegram 没有 test 端点：用 /integrations/telegram/status 的 enabled 如实表达。
      const res = await api.telegramStatus()
      const msg = res.data.enabled
        ? `通道已启用，白名单 ${res.data.allowlist} 人`
        : '通道未启用（需配置 bot token 且白名单非空）'
      channelResult.value = { ...channelResult.value, [key]: { ok: res.data.enabled, message: msg } }
    } else {
      const res = key === 'qb' ? await api.testQB() : await api.testN115()
      const msg = res.ok ? (res.version ? `连接正常（版本 ${res.version}）` : '连接正常') : '连接失败'
      channelResult.value = { ...channelResult.value, [key]: { ok: res.ok, message: msg } }
    }
  } catch (e) {
    channelResult.value = {
      ...channelResult.value,
      [key]: { ok: false, message: e instanceof Error ? e.message : '测试失败' },
    }
  } finally {
    testing.value = false
  }
}

async function createToken() {
  const name = newTokenName.value.trim()
  if (!name) {
    toast.warning('先给令牌起个名字')
    return
  }
  try {
    const res = await api.createToken(name, newTokenScope.value)
    newTokenPlain.value = res.token
    newTokenName.value = ''
    toast.success('令牌已签发（明文只显示这一次）')
    await loadTokens()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '签发失败')
  }
}

async function revokeToken(id: string) {
  try {
    await api.revokeToken(id)
    toast.success('已撤销（立即失效）')
    await loadTokens()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '撤销失败')
  }
}

async function copyToken() {
  if (!newTokenPlain.value) return
  try {
    await navigator.clipboard.writeText(newTokenPlain.value)
    toast.success('已复制到剪贴板')
  } catch {
    toast.warning('浏览器拒绝了剪贴板操作，请手动选中复制')
  }
}

// ---- 账号（用户名 / 密码修改，docs/04 §2.1）----
// 与配置分组相互独立：各自带保存按钮，不进 drafts/dirtyKeys 那套对账。
// 前端校验只做即时反馈，规则以后端为准（用户名 2–32 位字母数字/_-/中文；口令 ≥8 位）。
const newUsername = ref('')
const usernamePw = ref('')
const oldPw = ref('')
const newPw = ref('')
const confirmPw = ref('')
const savingName = ref(false)
const savingPw = ref(false)

/** 账号表单是否有未提交的输入（用于离开路由时的提示，与配置脏状态同一拦截）。 */
const accountDirty = computed(
  () => !!(newUsername.value || usernamePw.value || oldPw.value || newPw.value || confirmPw.value),
)

/** 用户名前端校验：与后端 validateUsername 同规则（错误文案也一致，避免两套说法）。 */
function usernameError(name: string): string {
  const v = name.trim()
  if (v.length < 2 || v.length > 32) return '用户名长度需在 2–32 之间'
  if (!/^[a-zA-Z0-9_\-\u3400-\u4DBF\u4E00-\u9FFF\uF900-\uFAFF]+$/.test(v)) {
    return '用户名仅限字母/数字/_/-/中文'
  }
  return ''
}

function passwordError(pw: string): string {
  if (pw && pw.length < 8) return '口令至少 8 位'
  return ''
}

async function saveUsername() {
  const name = newUsername.value.trim()
  const err = usernameError(name)
  if (err) {
    toast.error(err)
    return
  }
  if (!usernamePw.value) {
    toast.error('请输入当前口令')
    return
  }
  savingName.value = true
  try {
    await auth.changeUsername(name, usernamePw.value)
    toast.success(`用户名已改为 ${name}`)
    newUsername.value = ''
    usernamePw.value = ''
  } catch {
    // 后端文案（口令不正确/用户名已存在/非法用户名）经 store.error 上抛，这里透传
    toast.error(auth.error || '用户名更新失败')
  } finally {
    savingName.value = false
  }
}

async function savePassword() {
  if (!oldPw.value) {
    toast.error('请输入当前口令')
    return
  }
  const err = passwordError(newPw.value)
  if (err) {
    toast.error(err)
    return
  }
  if (newPw.value !== confirmPw.value) {
    toast.error('两次输入的新口令不一致')
    return
  }
  savingPw.value = true
  try {
    await auth.changePassword(oldPw.value, newPw.value)
    toast.success('密码已修改，当前登录态保持')
    oldPw.value = ''
    newPw.value = ''
    confirmPw.value = ''
  } catch {
    toast.error(auth.error || '密码更新失败')
  } finally {
    savingPw.value = false
  }
}

// 脏状态拦截（仅表单脏时拦路由离开）。
onBeforeRouteLeave(() => {
  if (isDirty.value || accountDirty.value) {
    return window.confirm('有未保存的改动，确定离开吗？')
  }
  return true
})

watch(historyKey, () => void loadHistory())

onMounted(load)

// 离开页面时收回保存反馈定时器，别让回调打到已卸载的组件上。
onBeforeUnmount(() => {
  if (hintTimer) clearTimeout(hintTimer)
})
</script>

<template>
  <section class="page">
    <header class="page-head">
      <h1 class="page-title">设置</h1>
      <div class="head-actions">
        <span v-if="isDirty" class="dirty-dot" title="有未保存的改动">● 未保存</span>
        <RButton variant="accent" :disabled="!isDirty || saving" @click="save">
          {{ saving ? '保存中…' : '保存改动' }}
        </RButton>
      </div>
    </header>

    <div v-if="errorMsg" class="error-card" role="alert">
      <p>{{ errorMsg }}</p>
      <RButton size="sm" @click="load">重试</RButton>
    </div>

    <div v-else-if="loading" class="skeleton-rows">
      <div v-for="n in 5" :key="n" class="skeleton-row" />
    </div>

    <template v-else>
      <div class="layout">
        <SettingsNav :sections="navSections" />

        <div class="content">
          <!-- 账号（用户名 / 密码）：与配置分组相互独立，各自即时保存 -->
          <article id="account" class="group">
            <h2 class="group-title">账号</h2>
            <p class="group-desc">
              本机单管理员账号。修改用户名或密码都需要验证当前口令；改密码不会踢掉当前登录。
            </p>

            <div class="acct-row">
              <div class="acct-meta">
                <span class="acct-label">用户名</span>
                <span class="acct-cur">{{ auth.user?.username || '—' }}</span>
              </div>
              <div class="acct-form">
                <RInput
                  v-model="newUsername"
                  class="acct-field"
                  label="新用户名"
                  placeholder="2–32 位，字母/数字/_/-/中文"
                  autocomplete="username"
                />
                <RInput
                  v-model="usernamePw"
                  class="acct-field"
                  type="password"
                  label="当前口令"
                  placeholder="验证身份"
                  autocomplete="current-password"
                />
                <RButton
                  variant="accent"
                  size="sm"
                  :disabled="savingName || !newUsername.trim() || !usernamePw"
                  @click="saveUsername"
                >
                  {{ savingName ? '保存中…' : '修改用户名' }}
                </RButton>
              </div>
            </div>

            <div class="acct-row">
              <div class="acct-meta">
                <span class="acct-label">密码</span>
                <span class="acct-cur">已设置（Argon2id 散列存储，不可查看）</span>
              </div>
              <div class="acct-form">
                <RInput
                  v-model="oldPw"
                  class="acct-field"
                  type="password"
                  label="当前口令"
                  autocomplete="current-password"
                />
                <RInput
                  v-model="newPw"
                  class="acct-field"
                  type="password"
                  label="新口令"
                  placeholder="至少 8 位"
                  autocomplete="new-password"
                />
                <RInput
                  v-model="confirmPw"
                  class="acct-field"
                  type="password"
                  label="确认新口令"
                  autocomplete="new-password"
                />
                <RButton
                  variant="accent"
                  size="sm"
                  :disabled="savingPw || !oldPw || !newPw || !confirmPw"
                  @click="savePassword"
                >
                  {{ savingPw ? '保存中…' : '修改密码' }}
                </RButton>
              </div>
            </div>
          </article>

          <article v-for="g in grouped" :id="g.key" :key="g.key" class="group">
            <h2 class="group-title">{{ g.label }}</h2>
            <SettingItem
              v-for="item in g.items"
              :key="item.key"
              :item="item"
              :model-value="draftOf(item)"
              :dirty="dirtyKeys.has(item.key)"
              :saved-text="savedHints[item.key]"
              @update:model-value="(v) => setDraft(item, v)"
            />
          </article>

          <!-- 外部集成通道测试 -->
          <article id="conn" class="group">
            <h2 class="group-title">外部集成通道测试</h2>
            <p class="group-desc">
              用服务端已保存的配置实探一次外部通道；未配置会如实报「未配置」，不假装可用。
            </p>
            <div class="conn-row">
              <RButton size="sm" :disabled="testing" @click="testChannel('qb')">测 qBittorrent</RButton>
              <RButton size="sm" :disabled="testing" @click="testChannel('n115')">测 115</RButton>
              <RButton size="sm" :disabled="testing" @click="testChannel('telegram')">查 Telegram 状态</RButton>
            </div>
            <p
              v-for="(r, k) in channelResult"
              :key="k"
              class="conn-result"
              :class="r.ok ? 'ok' : 'bad'"
            >
              {{ r.ok ? '✓' : '✕' }} {{ r.message }}
            </p>
          </article>

          <!-- 诊断入口（方案 §3「诊断入口在设置页」） -->
          <article id="diag" class="group">
            <h2 class="group-title">诊断</h2>
            <DiagPanel @error="(m) => toast.error(m)" />
          </article>

          <!-- API Token -->
          <article id="tokens" class="group">
            <h2 class="group-title">API 令牌</h2>
            <p class="group-desc">
              给 CLI、扩展、快捷指令用。明文只显示一次；撤销后立即失效。
            </p>
            <div class="token-form">
              <input v-model="newTokenName" class="input" placeholder="令牌名称，如 my-laptop" />
              <RSelect
                v-model="newTokenScope"
                label="权限范围"
                :options="[
                  { value: 1, label: '只读' },
                  { value: 3, label: '读写' },
                  { value: 7, label: '管理（含配置与凭据）' },
                ]"
              />
              <RButton variant="accent" size="sm" @click="createToken">签发</RButton>
            </div>
            <div v-if="newTokenPlain" class="token-plain">
              <code>{{ newTokenPlain }}</code>
              <RButton size="sm" @click="copyToken">复制</RButton>
            </div>
            <ul v-if="tokens.length" class="token-list">
              <li v-for="t in tokens" :key="t.id" class="token-row">
                <div class="token-main">
                  <span class="token-name">{{ t.name }}</span>
                  <span class="token-scope tnum">scope {{ t.scope }}</span>
                  <span class="token-time tnum">
                    {{ t.revoked ? '已撤销' : '创建于 ' + formatRelative(t.created_at) }}
                  </span>
                </div>
                <RButton v-if="!t.revoked" size="sm" tone="danger" @click="revokeToken(t.id)">撤销</RButton>
              </li>
            </ul>
            <p v-else class="muted">还没有签发过令牌。</p>
          </article>

          <!-- 配置历史 -->
          <article id="history" class="group">
            <h2 class="group-title">配置历史</h2>
            <div class="history-filter">
              <input
                v-model="historyKey"
                class="input"
                placeholder="按配置键过滤，如 download.max_concurrent"
                @keyup.enter="loadHistory"
              />
              <RButton size="sm" @click="loadHistory">刷新</RButton>
            </div>
            <ul v-if="history.length" class="history-list">
              <li v-for="h in history" :key="h.id" class="history-row">
                <div class="history-main">
                  <span class="history-key">{{ h.key }}</span>
                  <span class="history-val tnum">v{{ h.version }}</span>
                  <span class="history-time tnum">{{ formatRelative(h.changed_at) }}</span>
                  <span class="history-by">{{ h.changed_by || '系统' }}</span>
                </div>
                <RButton size="sm" @click="rollback(h)">回滚到此版本</RButton>
              </li>
            </ul>
            <REmpty v-else face="waiting" title="还没有配置历史" hint="每次保存设置都会在这里留痕。" />
          </article>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.page-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 18px; }
.page-title { font-size: var(--font-title-1); font-weight: var(--weight-title); border-left: 4px solid var(--gold); padding-left: 12px; }
.head-actions { display: flex; align-items: center; gap: 10px; }
.dirty-dot { color: var(--warning); font-size: var(--font-aux); }
/* 左分组导航在 SettingsNav 组件内（含它的响应式折行规则）；
   这里只留两栏布局本身。 */
.layout { display: grid; grid-template-columns: 180px 1fr; gap: 28px; align-items: start; }

/* 账号分组：左侧说明当前值，右侧表单；窄屏折成上下两行 */
.acct-row {
  display: flex; align-items: flex-start; gap: 20px; flex-wrap: wrap;
  padding: 14px 0; border-top: 1px solid var(--border);
}
.acct-row:first-of-type { border-top: none; }
.acct-meta { flex: 0 0 180px; display: flex; flex-direction: column; gap: 4px; padding-top: 4px; }
.acct-label { font-size: var(--font-body); font-weight: var(--weight-semibold); color: var(--text-1); }
.acct-cur { font-size: var(--font-aux); color: var(--text-3); }
.acct-form { flex: 1; min-width: 260px; display: flex; flex-wrap: wrap; gap: 12px; align-items: flex-end; }
.acct-field { flex: 1 1 180px; min-width: 0; }
.group { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-card); padding: 20px 24px; margin-bottom: 18px; box-shadow: var(--shadow-1); }
.group-title { font-size: var(--font-title-2); font-weight: var(--weight-strong); margin-bottom: 6px; }
/* 分组说明是正文档：它解释这组配置管什么，用户不看就改不了。 */
.group-desc { font-size: var(--font-body); color: var(--text-3); margin-bottom: 16px; line-height: 1.7; }
/* 配置项（.setting 及其控件）在 SettingItem 组件内。 */
.input {
  padding: 8px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--surface);
  color: var(--text-1);
  font-size: var(--font-body);
  min-width: 180px;
}
.input:disabled { background: var(--surface-2); color: var(--text-3); }
.conn-row { display: flex; gap: 8px; flex-wrap: wrap; }
.conn-result { margin-top: 10px; font-size: var(--font-aux); }
.conn-result.ok { color: var(--success); }
.conn-result.bad { color: var(--danger); }
.token-form { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 10px; }
.token-plain { display: flex; align-items: center; gap: 8px; background: var(--gold-soft); padding: 8px 10px; border-radius: var(--radius-ctrl); margin-bottom: 12px; }
.token-plain code { flex: 1; font-size: var(--font-aux); word-break: break-all; color: var(--text-1); }
.token-list, .history-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
.token-row, .history-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 8px 10px; background: var(--surface-2); border-radius: var(--radius-ctrl); }
.token-main, .history-main { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; font-size: var(--font-aux); }
.token-name, .history-key { color: var(--text-1); font-weight: var(--weight-caption); }
.token-scope, .history-val, .history-time, .history-by { font-size: var(--font-caption); color: var(--text-3); }
.history-filter { display: flex; gap: 8px; margin-bottom: 12px; }
.muted { font-size: var(--font-aux); color: var(--text-3); }
.error-card { background: var(--surface); border: 1px solid var(--danger); border-radius: var(--radius-card); padding: 16px 20px; color: var(--danger); display: flex; align-items: center; gap: 12px; }
.skeleton-rows { display: flex; flex-direction: column; gap: 10px; }
.skeleton-row { height: 44px; border-radius: var(--radius-ctrl); background: var(--surface-2); }
@media (max-width: 1024px) {
  .layout { grid-template-columns: 1fr; }
}
</style>
