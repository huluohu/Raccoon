<script setup lang="ts">
// 连接页（docs/07 §3 Connections / docs/04 §2.5）：凭据中心。
//
// 铁律：前端永远拿不到明文，只能拿到 credential.Mask 的打码预览；
// 换绑时留空 value 表示不改内容，提交空值等于「保持原样」而非「清空」。
// 凭据类目由 kind 决定（cookie_file 粘 Cookie 原文，token/session 粘令牌串）。
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import { useToastStore } from '@/stores/toast'
import { formatRelative } from '@/utils/format'
import type { Credential } from '@/api/types'
import RButton from '@/components/base/RButton.vue'
import REmpty from '@/components/base/REmpty.vue'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import PlatformBadge from '@/components/task/PlatformBadge.vue'

const toast = useToastStore()

const items = ref<Credential[]>([])
const loading = ref(true)
const loadErr = ref('')

/** 表单：新建与编辑共用。 */
const formOpen = ref(false)
const editing = ref<Credential | null>(null)
const saving = ref(false)
const form = ref({
  platform: 'bilibili',
  kind: 'cookie_file',
  label: '',
  note: '',
  value: '',
  expires_at: '',
})

/** 正在做健康检查的凭据 id。 */
const checking = ref('')
/** 检查结果缓存：id → { ok, status, message }。 */
const checkResult = ref<Record<string, { ok: boolean; status: string; message: string }>>({})

const PLATFORMS = [
  { value: 'bilibili', label: '哔哩哔哩' },
  { value: 'telegram', label: 'Telegram' },
  { value: 'n115', label: '115 网盘' },
  { value: 'qbittorrent', label: 'qBittorrent' },
  { value: 'douyu', label: '斗鱼' },
]

const KINDS = [
  { value: 'cookie_file', label: 'Cookie 文件内容', hint: '浏览器 Cookie 原文，等号分号分隔的一整串' },
  { value: 'account', label: '账号密码', hint: '用于需要登录的平台' },
  { value: 'token', label: '令牌', hint: 'Bot Token / API Key' },
  { value: 'session', label: '会话串', hint: '服务端下发的会话标识' },
]

const STATUS_TEXT: Record<string, string> = {
  active: '可用',
  expiring: '待确认',
  expired: '已失效',
  disabled: '已停用',
}
const STATUS_TONE: Record<string, string> = {
  active: 'var(--success)',
  expiring: 'var(--warning)',
  expired: 'var(--danger)',
  disabled: 'var(--neutral)',
}
const KIND_TEXT: Record<string, string> = {
  cookie_file: 'Cookie',
  account: '账号',
  token: '令牌',
  session: '会话',
}

const kindHint = computed(() => KINDS.find((k) => k.value === form.value.kind)?.hint ?? '')
const isEditing = computed(() => editing.value !== null)
/** 编辑态下 value 留空 = 不改内容。 */
const valuePlaceholder = computed(() =>
  isEditing.value
    ? '留空表示不修改（当前 ' + (editing.value?.preview ?? '已设置') + '）'
    : kindHint.value,
)

const expiringSoon = computed(() =>
  items.value.filter((c) => c.status === 'expiring' || c.status === 'expired'),
)

async function load() {
  loading.value = true
  loadErr.value = ''
  try {
    const res = await api.listCredentials()
    items.value = res.items ?? []
  } catch (e) {
    loadErr.value = e instanceof Error ? e.message : '凭据加载失败'
  } finally {
    loading.value = false
  }
}

function resetForm() {
  form.value = { platform: 'bilibili', kind: 'cookie_file', label: '', note: '', value: '', expires_at: '' }
  editing.value = null
  formOpen.value = true
}

function openEdit(c: Credential) {
  editing.value = c
  form.value = {
    platform: c.platform,
    kind: c.kind,
    label: c.label,
    note: c.note ?? '',
    value: '',
    expires_at: c.expires_at ? new Date(c.expires_at).toISOString().slice(0, 10) : '',
  }
  formOpen.value = true
}

async function save() {
  if (!form.value.value.trim() && !isEditing.value) {
    toast.error('新建凭据必须填写内容')
    return
  }
  saving.value = true
  try {
    const expiresAt = form.value.expires_at
      ? new Date(form.value.expires_at + 'T23:59:59').getTime()
      : isEditing.value
        ? undefined
        : undefined
    if (isEditing.value && editing.value) {
      const body: { label?: string; note?: string; value?: string } = {
        label: form.value.label,
        note: form.value.note,
      }
      if (form.value.value.trim()) body.value = form.value.value
      await api.updateCredential(editing.value.id, body)
      toast.success('凭据已更新')
    } else {
      await api.createCredential({
        platform: form.value.platform,
        kind: form.value.kind,
        label: form.value.label,
        note: form.value.note,
        value: form.value.value,
        expires_at: expiresAt,
      })
      toast.success('凭据已加密保存')
    }
    formOpen.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}

async function check(c: Credential) {
  checking.value = c.id
  try {
    const res = await api.checkCredential(c.id)
    checkResult.value = { ...checkResult.value, [c.id]: res }
    if (res.ok) toast.success(c.label + '：' + res.message)
    else toast.warning(c.label + '：' + res.message)
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '检查失败')
  } finally {
    checking.value = ''
  }
}

async function remove(c: Credential) {
  try {
    await api.deleteCredential(c.id)
    toast.success('已删除 ' + c.label)
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '删除失败')
  }
}

function expireText(c: Credential): string {
  if (!c.expires_at) return '长期有效'
  const left = c.expires_at - Date.now()
  if (left <= 0) return '已过期 ' + formatRelative(c.expires_at)
  return formatRelative(c.expires_at) + '过期'
}

onMounted(load)
</script>

<template>
  <section class="page">
    <header class="page-head">
      <div>
        <h1 class="page-title">连接</h1>
        <p class="page-sub">平台凭据中心 · 内容 AES-256-GCM 加密落库，界面只回打码预览</p>
      </div>
      <RButton variant="accent" @click="resetForm">新建凭据</RButton>
    </header>

    <p v-if="expiringSoon.length" class="alert">
      有 {{ expiringSoon.length }} 条凭据已失效或待确认，失效后高清晰度与会员内容会下载失败。
    </p>

    <p v-if="loadErr" class="alert alert--danger">{{ loadErr }}</p>

    <REmpty
      v-else-if="!loading && items.length === 0"
      face="waiting"
      title="还没有配置任何凭据"
      hint="不配凭据也能下载公开内容；只有 1080P 以上、会员专享、自己的收藏夹才需要登录态。"
      action-label="新建第一条凭据"
      @action="resetForm"
    />

    <div v-else class="cred-grid">
      <article v-for="c in items" :key="c.id" class="card">
        <header class="card-head">
          <PlatformBadge :platform="c.platform" />
          <span class="dot" :style="{ background: STATUS_TONE[c.status] || 'var(--neutral)' }" aria-hidden="true" />
          <span class="status" :style="{ color: STATUS_TONE[c.status] }">{{ STATUS_TEXT[c.status] || c.status }}</span>
          <span class="kind">{{ KIND_TEXT[c.kind] || c.kind }}</span>
        </header>

        <h3 class="card-label">{{ c.label }}</h3>

        <dl class="meta">
          <div><dt>内容预览</dt><dd class="mono">{{ c.preview || '—' }}</dd></div>
          <div><dt>有效期</dt><dd>{{ expireText(c) }}</dd></div>
          <div><dt>上次检查</dt><dd>{{ c.last_check_at ? formatRelative(c.last_check_at) : '未检查' }}</dd></div>
        </dl>

        <p v-if="c.note" class="note">{{ c.note }}</p>
        <p v-if="checkResult[c.id]" class="check" :class="checkResult[c.id].ok ? 'ok' : 'bad'">
          {{ checkResult[c.id].ok ? '✓' : '!' }} {{ checkResult[c.id].message }}
        </p>

        <footer class="card-foot">
          <RButton size="sm" :disabled="checking === c.id" @click="check(c)">
            {{ checking === c.id ? '检查中…' : '健康检查' }}
          </RButton>
          <RButton size="sm" variant="ghost" @click="openEdit(c)">编辑</RButton>
          <RButton size="sm" variant="ghost" tone="danger" confirm="再点一次确认删除" @click="remove(c)">删除</RButton>
        </footer>
      </article>
    </div>

    <DetailDrawer :open="formOpen" :title="isEditing ? '编辑凭据' : '新建凭据'" @close="formOpen = false">
      <form class="form" @submit.prevent="save">
        <label class="field">
          <span class="field-label">平台</span>
          <select v-model="form.platform" :disabled="isEditing">
            <option v-for="p in PLATFORMS" :key="p.value" :value="p.value">{{ p.label }}</option>
          </select>
        </label>

        <label class="field">
          <span class="field-label">类型</span>
          <select v-model="form.kind" :disabled="isEditing">
            <option v-for="k in KINDS" :key="k.value" :value="k.value">{{ k.label }}</option>
          </select>
        </label>

        <label class="field">
          <span class="field-label">标签</span>
          <input v-model="form.label" type="text" placeholder="如：B站主号" maxlength="64" />
          <span class="field-hint">留空自动用「平台 + 类型」命名</span>
        </label>

        <label class="field">
          <span class="field-label">{{ isEditing ? '替换内容（可选）' : '凭据内容' }}</span>
          <textarea
            v-model="form.value"
            rows="6"
            spellcheck="false"
            autocomplete="off"
            :placeholder="valuePlaceholder"
          />
          <span class="field-hint">{{ isEditing ? '留空保留原值；填写即整体替换' : kindHint }}</span>
        </label>

        <label class="field">
          <span class="field-label">到期日（可选）</span>
          <input v-model="form.expires_at" type="date" />
          <span class="field-hint">用于提醒 Cookie 何时失效，留空表示长期有效</span>
        </label>

        <label class="field">
          <span class="field-label">备注（可选）</span>
          <input v-model="form.note" type="text" placeholder="如：主号，PASS 全部" maxlength="200" />
        </label>

        <div class="form-foot">
          <RButton variant="ghost" @click="formOpen = false">取消</RButton>
          <RButton variant="accent" type="submit" :disabled="saving">{{ saving ? '保存中…' : '保存' }}</RButton>
        </div>
      </form>
    </DetailDrawer>
  </section>
</template>

<style scoped>
.page-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
.page-title { font-size: 24px; font-weight: 750; border-left: 4px solid var(--gold); padding-left: 12px; }
.page-sub { margin-top: 6px; font-size: var(--font-aux); color: var(--text-3); }
.alert {
  padding: 10px 14px;
  margin-bottom: 14px;
  border-radius: var(--radius-ctrl);
  background: var(--gold-soft);
  border: 1px solid var(--border);
  font-size: var(--font-aux);
  color: var(--text-2);
}
.alert--danger { background: transparent; border-color: var(--danger); color: var(--danger); }

.cred-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 12px; }
.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  box-shadow: var(--shadow-1);
}
.card-head { display: flex; align-items: center; gap: 8px; }
.dot { width: 8px; height: 8px; border-radius: 50%; margin-left: auto; }
.status { font-size: var(--font-caption); font-weight: 600; }
.kind {
  font-size: var(--font-caption);
  color: var(--text-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 1px 8px;
}
.card-label { font-size: var(--font-title-3); font-weight: 650; }
.meta { display: flex; flex-direction: column; gap: 4px; }
.meta > div { display: flex; gap: 8px; font-size: var(--font-caption); }
.meta dt { color: var(--text-3); width: 68px; flex-shrink: 0; }
.meta dd { color: var(--text-2); }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.note { font-size: var(--font-caption); color: var(--text-3); }
.check { font-size: var(--font-caption); }
.check.ok { color: var(--success); }
.check.bad { color: var(--warning); }
.card-foot { display: flex; gap: 6px; margin-top: auto; padding-top: 6px; border-top: 1px solid var(--border); }

.form { display: flex; flex-direction: column; gap: 14px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field-label { font-size: var(--font-aux); font-weight: 600; color: var(--text-2); }
.field input, .field select, .field textarea {
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--bg);
  color: var(--text-1);
  font-size: var(--font-aux);
  font-family: inherit;
  width: 100%;
  box-sizing: border-box;
}
.field textarea { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; resize: vertical; }
.field input:focus, .field select:focus, .field textarea:focus { outline: 2px solid var(--gold); outline-offset: 1px; }
.field input:disabled, .field select:disabled { opacity: 0.6; }
.field-hint { font-size: var(--font-caption); color: var(--text-3); }
.form-foot { display: flex; justify-content: flex-end; gap: 8px; padding-top: 6px; border-top: 1px solid var(--border); }
</style>