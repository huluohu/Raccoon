<script setup lang="ts">
// 连接页（docs/07 §3 Connections / docs/04 §2.5）：与外部系统的集成通道。
//
// 职责边界（2026-10-24 用户裁决）：本页只管「系统集成」——qBittorrent /
// 115 / Telegram 的连接参数与凭据；内容平台（B站 / YouTube / 优酷…）的
// 登录态归「平台账号」页（/accounts），下载源账号不是「与系统的连接」。
//
// 铁律：前端永远拿不到明文，只能拿到 credential.Mask 的打码预览；
// 换绑时留空 value 表示不改内容，提交空值等于「保持原样」而非「清空」。
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import { useToastStore } from '@/stores/toast'
import type { ConfigItem, Credential, IntegrationChannelStatus, IntegrationStatus, TelegramStatus } from '@/api/types'
import { INTEGRATION_PLATFORMS } from '@/utils/platforms'
import RButton from '@/components/base/RButton.vue'
import RInput from '@/components/base/RInput.vue'
import PageHeader from '@/components/base/PageHeader.vue'
import REmpty from '@/components/base/REmpty.vue'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import CredentialList from '@/components/conn/CredentialList.vue'
import QbTestPanel from '@/components/conn/QbTestPanel.vue'
import TelegramPanel from '@/components/conn/TelegramPanel.vue'

const toast = useToastStore()

const items = ref<Credential[]>([])
const loading = ref(true)
const loadErr = ref('')

/** 只显示集成组件的凭据；内容平台账号在「平台账号」页。 */
const integrationCreds = computed(() =>
  items.value.filter((c) => (INTEGRATION_PLATFORMS as readonly string[]).includes(c.platform)),
)

/** 表单：新建与编辑共用。 */
const formOpen = ref(false)
const editing = ref<Credential | null>(null)
const saving = ref(false)
const form = ref({
  platform: 'telegram',
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
  { value: 'telegram', label: 'Telegram' },
  { value: 'n115', label: '115 网盘' },
  { value: 'qbittorrent', label: 'qBittorrent' },
]

const KINDS = [
  { value: 'cookie_file', label: 'Cookie 文件内容', hint: '浏览器 Cookie 原文，等号分号分隔的一整串' },
  { value: 'account', label: '账号密码', hint: '用于需要登录的平台' },
  { value: 'token', label: '令牌', hint: 'Bot Token / API Key' },
  { value: 'session', label: '会话串', hint: '服务端下发的会话标识' },
]

const kindHint = computed(() => KINDS.find((k) => k.value === form.value.kind)?.hint ?? '')
const isEditing = computed(() => editing.value !== null)
/** 编辑态下 value 留空 = 不改内容。 */
const valuePlaceholder = computed(() =>
  isEditing.value
    ? '留空表示不修改（当前 ' + (editing.value?.preview ?? '已设置') + '）'
    : kindHint.value,
)

const expiringSoon = computed(() =>
  integrationCreds.value.filter((c) => c.status === 'expiring' || c.status === 'expired'),
)

// ---- 外部集成通道（docs/04 §2.10 / docs/08 W4.6–W4.8）----

/** GET /integrations/status 的三通道状态（未探测时 ok/message 缺省 = 状态不明）。 */
const integrations = ref<IntegrationStatus | null>(null)
const integrationsLoading = ref(true)
const integrationsErr = ref('')
/** GET /integrations/telegram/status：真实可用性靠它（enabled + 白名单人数）。 */
const tgStatus = ref<TelegramStatus | null>(null)
/** 正在测试的通道 key。 */
const testingChannel = ref('')
/** 测试结果：key → { ok, text }。 */
const channelTest = ref<Record<string, { ok: boolean; text: string }>>({})

/** 单通道展示态：如实区分「未配置 / 状态不明 / 可用 / 不可用」。 */
type ChannelState = 'unconfigured' | 'unknown' | 'ok' | 'bad'

interface ChannelView {
  state: ChannelState
  stateText: string
  detail: string
}

/**
 * 通道状态翻译。
 *
 * 四态必须分开，不能合并：
 *   - 未配置：后端说 configured=false → 用户还没填参数
 *   - 状态不明：configured=true 但没有实探结果（ok 缺省）
 *     → 后端只在能构造客户端时实探，没探到就是「不知道」，
 *       绝不能显示成「可用」——那是假装可用。
 *   - 可用 / 不可用：后端实探过了。
 */
function channelView(key: 'qb' | 'n115'): ChannelView {
  const st: IntegrationChannelStatus | undefined = integrations.value?.[key]
  if (!st || !st.configured) {
    return { state: 'unconfigured', stateText: '未配置', detail: '点「填写参数」就地配置后启用' }
  }
  if (st.ok === undefined) {
    return { state: 'unknown', stateText: '状态不明', detail: '已配置，尚未探测到实探结果' }
  }
  if (st.ok) {
    return { state: 'ok', stateText: '可用', detail: st.message || '实探通过' }
  }
  return { state: 'bad', stateText: '不可用', detail: st.message || '实探失败' }
}

const qbView = computed(() => channelView('qb'))
const n115View = computed(() => channelView('n115'))

async function loadIntegrations() {
  integrationsLoading.value = true
  integrationsErr.value = ''
  try {
    const res = await api.integrationStatus()
    integrations.value = res.data
  } catch (e) {
    integrationsErr.value = e instanceof Error ? e.message : '外部集成状态加载失败'
  } finally {
    integrationsLoading.value = false
  }
  // Telegram 的 enabled/allowlist 单独取（status 里的 telegram 只给 configured）。
  try {
    const res = await api.telegramStatus()
    tgStatus.value = res.data
  } catch {
    tgStatus.value = null
  }
}

// ---- 通道参数表单（参数在「连接」页就地填写；设置页不再放通道组）----
//
// 敏感项语义（与后端打码一致）：GET /config 对 sensitive 项返回 "xx****yy"，
// 表单里敏感项一律留空 = 保持不变；提交时跳过空值——绝不能把打码串或空串写回。

/** 通道 key → 表单字段定义。sensitive 字段用 password 输入 + 留空保持。 */
const CHANNEL_FIELDS: Record<string, { key: string; label: string; sensitive?: boolean; placeholder: string }[]> = {
  qb: [
    { key: 'qb.url', label: 'WebUI 地址', placeholder: '如 http://127.0.0.1:8080' },
    { key: 'qb.username', label: '用户名', placeholder: 'qB 的登录用户名' },
    { key: 'qb.password', label: '密码', sensitive: true, placeholder: 'qB 的登录密码' },
  ],
  n115: [
    { key: 'n115.client_id', label: '应用 ID（client_id）', placeholder: '115 开放平台应用 ID' },
    { key: 'n115.access_token', label: 'Access Token', sensitive: true, placeholder: '115 开放平台 access token' },
    { key: 'n115.refresh_token', label: 'Refresh Token', sensitive: true, placeholder: '115 开放平台 refresh token' },
  ],
  telegram: [
    { key: 'telegram.bot_token', label: 'Bot Token', sensitive: true, placeholder: '从 @BotFather 拿到的 token' },
    { key: 'telegram.allowed_users', label: '允许的用户 ID', placeholder: '逗号分隔，如 123,456；留空不限制' },
    { key: 'telegram.allowed_chats', label: '允许的会话 ID', placeholder: '逗号分隔；留空不限制' },
  ],
}

const CHANNEL_TITLES: Record<string, string> = {
  qb: 'qBittorrent 参数',
  n115: '115 网盘参数',
  telegram: 'Telegram 参数',
}

const paramsChannel = ref<'qb' | 'n115' | 'telegram' | null>(null)
const paramsLoading = ref(false)
const paramsSaving = ref(false)
/** 表单值：key → 字符串。敏感项加载时置空（占位提示当前是否已设置）。 */
const paramsForm = ref<Record<string, string>>({})
/** 敏感项当前是否已有值（决定占位文案「已设置 · 留空保持不变」）。 */
const sensitiveSet = ref<Record<string, boolean>>({})

const paramsFields = computed(() => (paramsChannel.value ? CHANNEL_FIELDS[paramsChannel.value] : []))

async function openParams(key: 'qb' | 'n115' | 'telegram') {
  paramsChannel.value = key
  paramsLoading.value = true
  paramsForm.value = {}
  sensitiveSet.value = {}
  try {
    const res = await api.getConfig()
    const items = res.items ?? []
    for (const f of CHANNEL_FIELDS[key]) {
      const it: ConfigItem | undefined = items.find((i) => i.key === f.key)
      if (f.sensitive) {
        sensitiveSet.value[f.key] = Boolean(it && typeof it.value === 'string' && it.value !== '')
        paramsForm.value[f.key] = ''
      } else {
        paramsForm.value[f.key] = typeof it?.value === 'string' ? it.value : ''
      }
    }
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '读取通道参数失败')
  } finally {
    paramsLoading.value = false
  }
}

async function saveParams() {
  if (!paramsChannel.value) return
  const key = paramsChannel.value
  const settings: Record<string, unknown> = {}
  for (const f of CHANNEL_FIELDS[key]) {
    const v = paramsForm.value[f.key]?.trim() ?? ''
    // 敏感项留空 = 保持不变（不打码串回写、也不清空）
    if (f.sensitive && v === '') continue
    settings[f.key] = v
  }
  if (Object.keys(settings).length === 0) {
    paramsChannel.value = null
    return
  }
  paramsSaving.value = true
  try {
    await api.saveConfig(settings)
    toast.success(`${CHANNEL_TITLES[key]}已保存，下个任务生效`)
    paramsChannel.value = null
    // 保存后立即实探一次：用户不用再手点「测试连接」
    if (key === 'qb' || key === 'n115') await testChannel(key)
    else await loadIntegrations()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '保存失败')
  } finally {
    paramsSaving.value = false
  }
}

async function testChannel(key: 'qb' | 'n115' | 'telegram') {
  // Telegram 没有独立的 test 端点：它的可用性由 /integrations/telegram/status 给出。
  if (key === 'telegram') {
    await loadIntegrations()
    toast.info('Telegram 通道没有测试端点，状态以左侧实测结果为准')
    return
  }
  testingChannel.value = key
  try {
    const res = key === 'qb' ? await api.testQB() : await api.testN115()
    const text = res.ok ? (res.version ? `连接成功（版本 ${res.version}）` : '连接成功') : '连接失败'
    channelTest.value = { ...channelTest.value, [key]: { ok: res.ok, text } }
    if (res.ok) toast.success((key === 'qb' ? 'qBittorrent' : '115') + '：' + text)
    else toast.warning((key === 'qb' ? 'qBittorrent' : '115') + '：' + text)
  } catch (e) {
    const text = e instanceof Error ? e.message : '测试失败'
    channelTest.value = { ...channelTest.value, [key]: { ok: false, text } }
    toast.error(text)
  } finally {
    testingChannel.value = ''
    await loadIntegrations()
  }
}

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

onMounted(() => {
  void load()
  void loadIntegrations()
})
</script>

<template>
  <section class="page">
    <PageHeader
      eyebrow="CHANNELS"
      title="连接"
      sub="与外部系统的集成：qBittorrent / 115 / Telegram · 内容平台的登录态在「平台账号」页"
    >
      <!-- 无「新建凭据」入口（2026-10-24 用户裁决）：集成参数走各通道的
           「填写参数」；内容平台凭据在「平台账号」页。 -->
    </PageHeader>

    <!-- 外部集成通道 -->
    <section class="integrations">
      <header class="int-head">
        <div>
          <h2 class="int-title">外部集成</h2>
          <p class="int-sub">qBittorrent / 115 / Telegram 三条通道的连接状态，实时探测，不假装可用</p>
        </div>
        <RButton size="sm" variant="ghost" :disabled="integrationsLoading" @click="loadIntegrations">
          {{ integrationsLoading ? '刷新中…' : '刷新状态' }}
        </RButton>
      </header>

      <p v-if="integrationsErr" class="alert alert--danger">{{ integrationsErr }}</p>

      <ul class="int-grid">
        <!-- qBittorrent 通道：主动探测（方案 §3 QbTestPanel） -->
        <QbTestPanel
          name="qBittorrent"
          desc="把磁力/离线下载交给 qB 客户端执行"
          :state="qbView.state"
          :detail="qbView.detail"
          :testing="testingChannel === 'qb'"
          :result="channelTest.qb ?? null"
          @test="testChannel('qb')"
          @params="openParams('qb')"
        />
        <!-- 115 网盘通道：主动探测 -->
        <QbTestPanel
          name="115 网盘"
          desc="把任务转存到 115 网盘"
          :state="n115View.state"
          :detail="n115View.detail"
          :testing="testingChannel === 'n115'"
          :result="channelTest.n115 ?? null"
          @test="testChannel('n115')"
          @params="openParams('n115')"
        />
        <!-- Telegram 通道：读配置状态，没有测试端点（方案 §3 TelegramPanel） -->
        <TelegramPanel
          :enabled="tgStatus?.enabled ?? false"
          :allowlist="tgStatus?.allowlist ?? 0"
          :loading="integrationsLoading"
          @refresh="loadIntegrations"
          @params="openParams('telegram')"
        />
      </ul>
    </section>

    <p v-if="expiringSoon.length" class="alert">
      有 {{ expiringSoon.length }} 条凭据已失效或待确认，失效后高清晰度与会员内容会下载失败。
    </p>

    <p v-if="loadErr" class="alert alert--danger">{{ loadErr }}</p>

    <REmpty
      v-else-if="!loading && integrationCreds.length === 0"
      face="waiting"
      title="还没有集成组件的凭据"
      hint="集成参数在上方各通道的「填写参数」里就地配置；内容平台的登录态在「平台账号」页。"
    />

    <!-- 凭据列表（方案 §3 视图级组件 CredentialList：只回打码预览） -->
    <CredentialList
      v-else
      :items="integrationCreds"
      :checking="checking"
      :results="checkResult"
      @check="check"
      @edit="openEdit"
      @remove="remove"
    />

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

    <!-- 通道参数抽屉：连接参数就地填写（设置页不再放通道组）。
         敏感项留空 = 保持不变；保存后自动实探一次。 -->
    <DetailDrawer
      :open="paramsChannel !== null"
      :title="paramsChannel ? CHANNEL_TITLES[paramsChannel] : '通道参数'"
      @close="paramsChannel = null"
    >
      <p class="params-note">
        参数保存在本机配置中心，保存后立即实探一次；实探结果如实显示，不假装可用。
      </p>
      <div v-if="paramsLoading" class="params-loading">读取中…</div>
      <form v-else-if="paramsChannel" class="params-form" @submit.prevent="saveParams">
        <RInput
          v-for="f in paramsFields"
          :key="f.key"
          v-model="paramsForm[f.key]"
          :label="f.label"
          :type="f.sensitive ? 'password' : 'text'"
          :placeholder="f.sensitive && sensitiveSet[f.key] ? '已设置 · 留空保持不变' : f.placeholder"
          :hint="f.sensitive ? '已设置时留空表示保持现有值不变' : ''"
          autocomplete="off"
        />
        <div class="params-actions">
          <RButton variant="ghost" type="button" @click="paramsChannel = null">取消</RButton>
          <RButton variant="accent" type="submit" :disabled="paramsSaving">
            {{ paramsSaving ? '保存中…' : '保存并测试' }}
          </RButton>
        </div>
      </form>
    </DetailDrawer>

  </section>
</template>

<style scoped>
/* 通道参数抽屉：与凭据抽屉同一套节奏（说明句 + 表单 + 底部动作区）。 */
.params-note { margin: 0 0 14px; font-size: var(--font-aux); color: var(--text-3); line-height: 1.7; }
.params-loading { padding: 24px 0; font-size: var(--font-aux); color: var(--text-3); }
.params-form { display: flex; flex-direction: column; gap: 14px; }
.params-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 6px; }

/* 页面副标题是正文档（16px）。 */
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

/* 凭据网格：auto-fit 让空轨道塌陷（auto-fill 会给 1 个凭据也留 3 条轨道）。 */

/* 外部集成通道 */
.integrations { margin-bottom: 20px; }
.int-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.int-title { margin: 0; font-size: var(--font-title-2); font-weight: var(--weight-strong); }
.int-sub { margin: 6px 0 0; font-size: var(--font-body); color: var(--text-3); }
.int-grid { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 16px; }
/* 通道状态：辅助档 13.5px（§2「12px 仅表头/徽章」；这是状态文字不是徽章）。 */
.int-test.ok { color: var(--success); }
.int-test.bad { color: var(--danger); }
.int-link:hover { color: var(--accent); }
/* 状态徽章是辅助信息，caption(12px) 合规。 */
.meta dt { color: var(--text-3); width: 72px; flex-shrink: 0; }
.meta dd { color: var(--text-2); }
.check.ok { color: var(--success); }
.check.bad { color: var(--warning); }

.form { display: flex; flex-direction: column; gap: 16px; }
.field { display: flex; flex-direction: column; gap: 6px; }
/* 表单字段名：正文档 16px。 */
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