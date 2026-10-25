<script setup lang="ts">
// SettingItem：单个配置项（方案 §3「/settings → SettingItem（生效时机徽章 + 脏圆点 + 保存反馈）」）。
//
// 为什么抽成组件：
// 1) 「生效时机」是这个页面的核心信息之一。生效时机由后端 `item.effective` 下发，
//    前端不猜（铁律：保存结果按接口返回的 effective_summary 展示）。
//    徽章一旦散落在视图里各处，很容易某处漏掉「需重启生效」这个提示——
//    而漏掉的后果是用户以为改完就生效了。
// 2) 「敏感项不回填掩码」是一条必须钉死的规则：
//    后端只回传掩码（`******`），前端如果把掩码当值回填进输入框，
//    用户点保存就会把掩码字面量写进配置。抽出来后这条规则有独立的用例守着。
// 3) 「env 锁定项只读」同理：被环境变量锁住的配置改不动，界面必须如实表现为只读。
//
// 组件只负责渲染与收集草稿值，不发请求（数据流走 api 层，§8 约定）：
// 保存由父组件统一提交，这样「哪些项被改了」只有一处判断。
import { computed, ref } from 'vue'
import { api } from '@/api/client'
import RButton from '@/components/base/RButton.vue'
import RSelect from '@/components/base/RSelect.vue'
import type { ConfigItem, Effective } from '@/api/types'

const props = defineProps<{
  /** 后端下发的配置项。 */
  item: ConfigItem
  /** 当前草稿值（父组件持有；敏感项为 '' 表示「不修改」）。 */
  modelValue: unknown
  /** 是否已修改未保存（父组件根据草稿与原值比对得出）。 */
  dirty: boolean
  /** 保存反馈（最近一次保存后，该项按后端下发的生效时机生效）；空 = 无反馈。 */
  savedText?: string
}>()

const emit = defineEmits<{
  /** 草稿值变化。 */
  'update:modelValue': [value: unknown]
}>()

/** 生效时机文案（后端 effective 是唯一来源，前端不自行推断）。 */
const EFFECTIVE_TEXT: Record<Effective, string> = {
  immediate: '立即生效',
  next_task: '下个任务生效',
  restart: '需重启生效',
}

const effectiveText = computed(() => EFFECTIVE_TEXT[props.item.effective])

/** 敏感项当前值的只读展示：后端只回掩码，空串 = 未设置（不显示明文）。 */
const sensitiveMask = computed(() => {
  const v = typeof props.item.value === 'string' ? props.item.value : ''
  return v ? v : '未设置'
})

/** 枚举项的值域由后端 choices 定义，用户不必猜合法值。 */
const choices = computed(() =>
  (props.item.choices ?? []).map((c) => ({ value: c, label: c })),
)

function onText(ev: Event) {
  const raw = (ev.target as HTMLInputElement).value
  if (props.item.type === 'int') {
    const n = Number(raw)
    // 空串回落到原值：int 项清空不等于「设成 0」。
    emit('update:modelValue', raw === '' ? props.item.value : Number.isNaN(n) ? raw : n)
  } else {
    emit('update:modelValue', raw)
  }
}

function onBool(ev: Event) {
  emit('update:modelValue', (ev.target as HTMLInputElement).checked)
}

const testingProxy = ref(false)
const proxyTestResult = ref<{ ok: boolean; message: string } | null>(null)

async function testProxy() {
  testingProxy.value = true
  proxyTestResult.value = null
  try {
    const proxyVal = String(props.modelValue ?? props.item.value ?? '')
    const res = await api.testConnection('proxy', { proxy: proxyVal })
    proxyTestResult.value = res
  } catch (e: unknown) {
    proxyTestResult.value = {
      ok: false,
      message: e instanceof Error ? e.message : '测试失败',
    }
  } finally {
    testingProxy.value = false
  }
}
</script>

<template>
  <div class="setting">
    <div class="setting-head">
      <!-- 主标题用中文显示名（label）；键名降为次要信息——
           排障时需要知道确切键名，但它不该是用户第一眼看到的东西。 -->
      <span class="setting-label">{{ item.label || item.key }}</span>
      <span class="setting-key-hint">{{ item.key }}</span>
      <span class="eff-badge" :class="`eff-badge--${item.effective}`">{{ effectiveText }}</span>
      <span v-if="item.locked" class="lock-badge" :title="`由环境变量 ${item.env_var} 锁定`">
        <!-- SVG 线稿锁：docs/07 §4/§5 禁 emoji 冒充图标 -->
        <svg class="lock-icon" viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
          <rect x="3.5" y="7" width="9" height="7" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.4" />
          <path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
        </svg>
        {{ item.env_var }}
      </span>
      <span v-if="item.sensitive" class="sens-badge">敏感</span>
      <span v-if="dirty" class="dirty-pip" title="已修改未保存" />
      <span v-if="savedText" class="saved-badge">{{ savedText }}</span>
    </div>
    <p class="setting-desc">{{ item.desc }}</p>
    <div class="setting-control">
      <template v-if="item.locked">
        <!-- env 锁定的项只读；敏感项即使锁定也只回显掩码，不显示明文。 -->
        <input
          v-if="item.sensitive"
          class="input"
          type="password"
          :value="''"
          disabled
          placeholder="由环境变量锁定"
        />
        <span v-if="item.sensitive" class="sens-state">当前：{{ sensitiveMask }}</span>
        <input v-else class="input" type="text" :value="String(item.value ?? '')" disabled />
      </template>
      <template v-else-if="item.sensitive">
        <!-- 敏感项：password 输入框 + 掩码展示；留空 = 不修改（不回填掩码，避免把掩码写回配置） -->
        <input
          class="input"
          type="password"
          autocomplete="new-password"
          :value="String(modelValue ?? '')"
          placeholder="留空表示不修改"
          @input="onText"
          @change="onText"
        />
        <span class="sens-state">当前：{{ sensitiveMask }}</span>
      </template>
      <template v-else-if="item.type === 'bool'">
        <label class="switch">
          <input type="checkbox" :checked="modelValue === true" @change="onBool" />
          <span>{{ modelValue === true ? '开' : '关' }}</span>
        </label>
      </template>
      <template v-else-if="item.type === 'int'">
        <input
          class="input input--num tnum"
          type="number"
          :value="String(modelValue ?? '')"
          @input="onText"
          @change="onText"
        />
      </template>
      <template v-else-if="choices.length > 0">
        <!-- 枚举项：值域由后端 choices 定义，用户不必猜合法值（前端绕过后端仍会拦） -->
        <RSelect
          :model-value="String(modelValue ?? '')"
          :label="item.desc"
          :options="choices"
          @update:model-value="(v) => emit('update:modelValue', String(v))"
        />
      </template>
      <template v-else>
        <input
          class="input"
          type="text"
          :value="String(modelValue ?? '')"
          @input="onText"
          @change="onText"
        />
      </template>
      <span v-if="!item.sensitive" class="setting-default tnum">默认 {{ String(item.default) }}</span>
      <div v-if="item.key === 'net.proxy'" class="proxy-test-box">
        <RButton size="sm" variant="ghost" :disabled="testingProxy" @click="testProxy">
          {{ testingProxy ? '测试中…' : '测试代理连接' }}
        </RButton>
        <span
          v-if="proxyTestResult"
          class="proxy-test-res"
          :class="proxyTestResult.ok ? 'is-ok' : 'is-bad'"
        >
          {{ proxyTestResult.message }}
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.setting { padding: 14px 0; border-bottom: 1px solid var(--border); }
.setting:last-child { border-bottom: none; }
.setting-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
/* 配置项中文名：正文档 16px，它是这一行的主角。 */
.setting-label { font-size: var(--font-body); font-weight: var(--weight-semibold); color: var(--text-1); }
/* 键名（等宽）：次要信息，只为排障时能对上后端注册表。 */
.setting-key-hint {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: var(--font-caption);
  color: var(--text-3);
}
/* 配置项说明：辅助档 13.5px。 */
.setting-desc { font-size: var(--font-aux); color: var(--text-3); margin: 4px 0 10px; line-height: 1.6; }
.setting-control { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.setting-default { font-size: var(--font-aux); color: var(--text-3); }
.proxy-test-box {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  margin-top: 6px;
  flex-wrap: wrap;
}
.proxy-test-res {
  font-size: var(--font-aux);
  line-height: 1.4;
}
.proxy-test-res.is-ok {
  color: var(--success);
}
.proxy-test-res.is-bad {
  color: var(--danger);
}
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
.input--num { width: 110px; min-width: 110px; }
.switch { display: inline-flex; align-items: center; gap: 6px; font-size: var(--font-aux); color: var(--text-2); }
.eff-badge { font-size: var(--font-caption); padding: 1px 7px; border-radius: var(--radius-pill); background: var(--surface-2); color: var(--text-2); }
.eff-badge--immediate { background: color-mix(in srgb, var(--success) 16%, transparent); color: var(--success); }
.eff-badge--next_task { background: color-mix(in srgb, var(--info) 16%, transparent); color: var(--info); }
.eff-badge--restart { background: color-mix(in srgb, var(--warning) 18%, transparent); color: var(--warning); }
.lock-badge { font-size: var(--font-caption); padding: 1px 7px; border-radius: var(--radius-pill); background: var(--surface-3); color: var(--text-2); display: inline-flex; align-items: center; gap: 4px; }
.lock-icon { flex-shrink: 0; }
.sens-badge { font-size: var(--font-caption); padding: 1px 7px; border-radius: var(--radius-pill); background: var(--gold-soft); color: var(--gold-strong); }
.sens-state { font-size: var(--font-caption); color: var(--text-3); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.dirty-pip { width: 7px; height: 7px; border-radius: 50%; background: var(--warning); }
/* 保存反馈：保存后按后端下发的生效时机如实告知这一项什么时候生效。 */
.saved-badge { font-size: var(--font-caption); padding: 1px 7px; border-radius: var(--radius-pill); background: color-mix(in srgb, var(--success) 16%, transparent); color: var(--success); }
</style>
