<script setup lang="ts">
// 平台账号页：内容平台（下载源）的登录态管理——B站扫码 / 其它平台粘贴 Cookie。
//
// 与「连接」页的职责边界（2026-10-24 用户裁决）：连接页管「与系统集成」（qB /
// 115 / Telegram），本页管「内容源账号」（B站 / YouTube / 优酷…的登录态）。
// 凭据同表存储（credentials，AES-256-GCM），按 platform 归属拆分展示；
// 引擎注入逻辑不区分页面：任务执行时按平台取最近有效的凭据。
//
// 铁律与连接页同源：前端永远拿不到明文，只能拿到打码预览；编辑留空 value = 不改内容。
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import { useToastStore } from '@/stores/toast'
import type { Credential } from '@/api/types'
import { CONTENT_PLATFORMS, PLATFORM_LABELS } from '@/utils/platforms'
import RButton from '@/components/base/RButton.vue'
import PageHeader from '@/components/base/PageHeader.vue'
import REmpty from '@/components/base/REmpty.vue'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import CredentialList from '@/components/conn/CredentialList.vue'
import BiliQrLogin from '@/components/conn/BiliQrLogin.vue'

const toast = useToastStore()

const items = ref<Credential[]>([])
const loading = ref(true)
const loadErr = ref('')

/** 只管内容平台的账号；集成组件（Telegram/115/qB）在「连接」页。 */
const contentCreds = computed(() => items.value.filter((c) => (CONTENT_PLATFORMS as readonly string[]).includes(c.platform)))

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

const checking = ref('')
const checkResult = ref<Record<string, { ok: boolean; status: string; message: string }>>({})

const PLATFORM_OPTIONS = CONTENT_PLATFORMS.map((p) => ({ value: p, label: PLATFORM_LABELS[p] }))

const KINDS = [
  { value: 'cookie_file', label: 'Cookie 内容', hint: '浏览器 Cookie 原文，等号分号分隔的一整串；B站可直接用上方扫码登录' },
  { value: 'session', label: '会话串', hint: '服务端下发的会话标识' },
  { value: 'token', label: '令牌', hint: '平台 API Key / 访问令牌' },
  { value: 'account', label: '账号密码', hint: '用于需要登录的平台' },
]

const kindHint = computed(() => KINDS.find((k) => k.value === form.value.kind)?.hint ?? '')
const isEditing = computed(() => editing.value !== null)
const valuePlaceholder = computed(() =>
  isEditing.value ? '留空表示不修改（当前 ' + (editing.value?.preview ?? '已设置') + '）' : kindHint.value,
)

const expiringSoon = computed(() =>
  contentCreds.value.filter((c) => c.status === 'expiring' || c.status === 'expired'),
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
    const expiresAt = form.value.expires_at ? new Date(form.value.expires_at + 'T23:59:59').getTime() : undefined
    if (isEditing.value && editing.value) {
      const body: { label?: string; note?: string; value?: string } = { label: form.value.label, note: form.value.note }
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

onMounted(load)
</script>

<template>
  <section class="page">
    <PageHeader
      eyebrow="ACCOUNTS"
      title="平台账号"
      sub="内容平台的登录态：扫码 / Cookie，加密存储、下载时自动注入 · 与「连接」（系统集成）分开"
    >
      <template #actions>
        <BiliQrLogin @done="load" />
        <!-- 空态时隐藏：空态卡自带同名 CTA，两个同功能按钮不同屏（同直播页裁决） -->
        <RButton v-if="contentCreds.length > 0" variant="accent" @click="resetForm">添加平台凭证</RButton>
      </template>
    </PageHeader>

    <p v-if="expiringSoon.length" class="alert">
      有 {{ expiringSoon.length }} 个平台账号已失效或待确认——失效后高清、会员内容会下载失败，重新扫码或更新 Cookie 即可。
    </p>

    <p v-if="loadErr" class="alert alert--danger">{{ loadErr }}</p>

    <REmpty
      v-else-if="!loading && contentCreds.length === 0"
      face="waiting"
      title="还没有配置任何平台账号"
      hint="公开内容无需登录；1080P 以上、会员专享内容需要对应平台的登录态。B站可用扫码登录，其它平台粘贴 Cookie。"
      action-label="添加平台凭证"
      @action="resetForm"
    />

    <CredentialList
      v-else
      :items="contentCreds"
      :checking="checking"
      :results="checkResult"
      @check="check"
      @edit="openEdit"
      @remove="remove"
    />

    <DetailDrawer :open="formOpen" :title="isEditing ? '编辑平台账号' : '添加平台凭证'" @close="formOpen = false">
      <form class="form" @submit.prevent="save">
        <label class="field">
          <span class="field-label">平台</span>
          <select v-model="form.platform" :disabled="isEditing">
            <option v-for="p in PLATFORM_OPTIONS" :key="p.value" :value="p.value">{{ p.label }}</option>
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
          <input v-model="form.note" type="text" placeholder="如：主号" maxlength="200" />
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
.alert {
  padding: 12px 16px;
  margin-bottom: 16px;
  border-radius: var(--radius-ctrl);
  background: var(--gold-soft);
  border: 1px solid var(--border);
  font-size: var(--font-body);
  color: var(--text-2);
}
.alert--danger { background: transparent; border-color: var(--danger); color: var(--danger); }

.form { display: flex; flex-direction: column; gap: 16px; }
.field { display: flex; flex-direction: column; gap: 6px; }
.field-label { font-size: var(--font-body); font-weight: var(--weight-semibold); color: var(--text-2); }
.field input, .field select, .field textarea {
  padding: 9px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--bg);
  color: var(--text-1);
  font-size: var(--font-body);
  font-family: inherit;
  width: 100%;
  box-sizing: border-box;
}
.field textarea { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; resize: vertical; }
.field input:focus, .field select:focus, .field textarea:focus { outline: 2px solid var(--accent); outline-offset: 1px; }
.field input:disabled, .field select:disabled { opacity: 0.6; }
.field-hint { font-size: var(--font-aux); color: var(--text-3); }
.form-foot { display: flex; justify-content: flex-end; gap: 8px; padding-top: 6px; border-top: 1px solid var(--border); }
</style>
